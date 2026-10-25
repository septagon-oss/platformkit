package contracts

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
)

var (
	ErrInvalid      = errors.New("decide: invalid input")      // Correctable: fix input.
	ErrDenied       = errors.New("decide: access denied")      // Correctable: obtain current authority.
	ErrNotFound     = errors.New("decide: not found")          // Immutable: this identity/scope is absent.
	ErrClosed       = errors.New("decide: question closed")    // Immutable: questions cannot reopen.
	ErrStale        = errors.New("decide: stale revision")     // Correctable: reload before a new intent.
	ErrJoinDisabled = errors.New("decide: link join disabled") // Correctable: creator supplies another invitation.
	ErrInvite       = errors.New("decide: invalid invitation") // Immutable: replace this credential.
	ErrUndo         = errors.New("decide: invalid undo")       // Immutable: this credential cannot act here.
	ErrIntegrity    = errors.New("decide: inconsistent state") // Immutable: corrupt input is never partially accepted.
	ErrExhausted    = errors.New("decide: revision exhausted") // Immutable: create a new identity.
	ErrLastOption   = errors.New("decide: last active option") // Correctable: add another option first.
)

// BallotChange is a deterministic decision. NewID and at are supplied by the
// caller; unchanged decisions preserve the existing ballot and publish nothing.
type BallotChange struct {
	Vote       Vote
	Previous   *Value
	Changed    bool
	FirstMatch bool
}

// DecideVote checks current grants, membership, parent state and the actor's
// expected ballot revision. It returns zero on refusal and never mutates s.
// The SQL adapter must call it with a complete snapshot under its question lock.
func DecideVote(s Snapshot, actor Actor, in VoteInput, newID uuid.UUID, at time.Time) (BallotChange, error) {
	if !actor.Read || !actor.Vote || actor.PersonID == uuid.Nil {
		return BallotChange{}, ErrDenied
	}
	if err := consistent(s); err != nil {
		return BallotChange{}, err
	}
	if in.QuestionID != s.Question.ID {
		return BallotChange{}, ErrNotFound
	}
	if !participant(s, actor.PersonID) {
		return BallotChange{}, ErrDenied
	}
	if at.IsZero() || in.IfRevision < 0 || !valid(in.Value) {
		return BallotChange{}, ErrInvalid
	}
	if !accepting(s.Question, at) {
		return BallotChange{}, ErrClosed
	}
	option, ok := activeOption(s, in.OptionID)
	if !ok {
		return BallotChange{}, ErrNotFound
	}
	prior := ballot(s, in.OptionID, actor.PersonID)
	same := prior.Value != nil && *prior.Value == in.Value
	if in.IfRevision != prior.Revision {
		if same && prior.Revision > 0 && in.IfRevision == prior.Revision-1 {
			return BallotChange{Vote: prior}, nil
		}
		return BallotChange{}, ErrStale
	}
	if same {
		return BallotChange{Vote: prior}, nil
	}
	if prior.Revision == math.MaxInt64 {
		return BallotChange{}, ErrExhausted
	}
	at = at.UTC().Truncate(time.Microsecond)
	next := prior
	if next.Revision == 0 {
		if newID == uuid.Nil {
			return BallotChange{}, ErrInvalid
		}
		next.Base = entity.Base{ID: newID, TenantID: s.Question.TenantID, CreatedAt: at}
	}
	next.QuestionID, next.OptionID, next.PersonID = s.Question.ID, in.OptionID, actor.PersonID
	next.Value, next.At, next.UpdatedAt, next.Revision = new(in.Value), at, at, prior.Revision+1
	return changedBallot(s, option, prior, next), nil
}

// DecideUndo checks verified claims under the same scope/time rules as voting.
// It restores the previous value with a new revision; it never resets history.
func DecideUndo(s Snapshot, actor Actor, claims UndoClaims, at time.Time) (BallotChange, error) {
	if !actor.Read || !actor.Vote || actor.PersonID == uuid.Nil || !participant(s, actor.PersonID) {
		return BallotChange{}, ErrDenied
	}
	if err := consistent(s); err != nil {
		return BallotChange{}, err
	}
	if at.IsZero() {
		return BallotChange{}, ErrInvalid
	}
	if claims.TenantID != s.Question.TenantID || claims.QuestionID != s.Question.ID || claims.PersonID != actor.PersonID ||
		claims.PostRevision < 1 || !at.Before(claims.ExpiresAt) || (claims.Previous != nil && !valid(*claims.Previous)) {
		return BallotChange{}, ErrUndo
	}
	if !accepting(s.Question, at) {
		return BallotChange{}, ErrClosed
	}
	option, ok := activeOption(s, claims.OptionID)
	if !ok {
		return BallotChange{}, ErrNotFound
	}
	prior := ballot(s, claims.OptionID, actor.PersonID)
	if prior.Revision != claims.PostRevision {
		if prior.Revision > 0 && prior.Revision-1 == claims.PostRevision && equalValue(prior.Value, claims.Previous) {
			return BallotChange{Vote: prior}, nil
		}
		return BallotChange{}, ErrStale
	}
	if prior.Revision == math.MaxInt64 {
		return BallotChange{}, ErrExhausted
	}
	next := prior
	next.Value, next.Revision = cloneValue(claims.Previous), prior.Revision+1
	next.At = at.UTC().Truncate(time.Microsecond)
	next.UpdatedAt = next.At
	return changedBallot(s, option, prior, next), nil
}

func changedBallot(s Snapshot, option Option, prior, next Vote) BallotChange {
	s.Votes = slices.DeleteFunc(slices.Clone(s.Votes), func(v Vote) bool {
		return v.OptionID == next.OptionID && v.PersonID == next.PersonID
	})
	s.Votes = append(s.Votes, next)
	return BallotChange{Vote: next, Previous: cloneValue(prior.Value), Changed: true,
		FirstMatch: tally(s, option).Matched && option.FirstMatchedAt == nil}
}

func equalValue(a, b *Value) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// Rank returns all active options from a consistent snapshot, including those
// with no votes. Authority belongs to the service; malformed input returns nil.
func Rank(s Snapshot) ([]Result, error) {
	if err := consistent(s); err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(s.Options))
	for _, option := range s.Options {
		if option.RemovedAt == nil {
			out = append(out, tally(s, option))
		}
	}
	slices.SortFunc(out, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.Option.Position, b.Option.Position), compareID(a.Option.ID, b.Option.ID))
	})
	return out, nil
}

// ReadResults enforces read authority and returns one explicitly timed snapshot.
func ReadResults(s Snapshot, actor Actor, at time.Time) (Results, error) {
	if !actor.Read || actor.PersonID == uuid.Nil || (actor.PersonID != s.Question.CreatorID && !participant(s, actor.PersonID)) {
		return Results{}, ErrDenied
	}
	if at.IsZero() {
		return Results{}, ErrInvalid
	}
	options, err := Rank(s)
	if err != nil {
		return Results{}, err
	}
	return Results{QuestionID: s.Question.ID, QuestionRevision: s.Question.Revision,
		ObservedAt: at.UTC().Truncate(time.Microsecond), AcceptingVotes: accepting(s.Question, at), Options: options}, nil
}

// ReadDeck returns only this participant's unvoted active options. A closed
// question has no cards; its total and completed counts remain readable.
func ReadDeck(s Snapshot, actor Actor, at time.Time) (Deck, error) {
	if !actor.Read || !actor.Vote || actor.PersonID == uuid.Nil || !participant(s, actor.PersonID) {
		return Deck{}, ErrDenied
	}
	if err := consistent(s); err != nil {
		return Deck{}, err
	}
	if at.IsZero() {
		return Deck{}, ErrInvalid
	}
	out := Deck{QuestionID: s.Question.ID, AcceptingVotes: accepting(s.Question, at), Cards: []Card{}}
	for _, option := range s.Options {
		if option.RemovedAt != nil {
			continue
		}
		out.Total++
		vote := ballot(s, option.ID, actor.PersonID)
		if vote.Value != nil {
			out.Voted++
		} else if out.AcceptingVotes {
			out.Cards = append(out.Cards, Card{Option: option, VoteRevision: vote.Revision, Unvoted: true})
		}
	}
	slices.SortFunc(out.Cards, func(a, b Card) int {
		return cmp.Or(cmp.Compare(a.Option.Position, b.Option.Position), compareID(a.Option.ID, b.Option.ID))
	})
	out.Remaining = int64(len(out.Cards))
	return out, nil
}

// AdmitJoin checks already verified claims against locked current admission
// state. It returns whether a new participant is needed, never grants a role.
func AdmitJoin(s Snapshot, actor Actor, claims InviteClaims, expected int64, at time.Time) (bool, error) {
	if !actor.Read || !actor.Vote || actor.PersonID == uuid.Nil {
		return false, ErrDenied
	}
	if err := consistent(s); err != nil {
		return false, err
	}
	if expected != 0 || at.IsZero() {
		return false, ErrInvalid
	}
	q := s.Question
	if claims.TenantID != q.TenantID || claims.QuestionID != q.ID || claims.Generation <= 0 ||
		claims.Generation != q.InviteGeneration || q.InviteExpiresAt == nil ||
		!claims.ExpiresAt.Equal(*q.InviteExpiresAt) || !at.Before(claims.ExpiresAt) {
		return false, ErrInvite
	}
	if !q.AllowLinkJoin {
		return false, ErrJoinDisabled
	}
	if !accepting(q, at) {
		return false, ErrClosed
	}
	add := !participant(s, actor.PersonID)
	if add && q.Revision == math.MaxInt64 {
		return false, ErrExhausted
	}
	return add, nil
}

func accepting(q Question, at time.Time) bool {
	return q.Status == StatusOpen && (q.ClosesAt == nil || at.Before(*q.ClosesAt))
}

func participant(s Snapshot, person uuid.UUID) bool {
	return slices.ContainsFunc(s.Participants, func(p Participant) bool { return p.PersonID == person })
}

func activeOption(s Snapshot, id uuid.UUID) (Option, bool) {
	for _, option := range s.Options {
		if option.ID == id && option.RemovedAt == nil {
			return option, true
		}
	}
	return Option{}, false
}

func ballot(s Snapshot, option, person uuid.UUID) Vote {
	for _, vote := range s.Votes {
		if vote.OptionID == option && vote.PersonID == person {
			vote.Value = cloneValue(vote.Value)
			return vote
		}
	}
	return Vote{}
}

func cloneValue(value *Value) *Value {
	if value == nil {
		return nil
	}
	return new(*value)
}

func valid(value Value) bool { return value == Yes || value == Strong || value == No || value == Skip }

func compareID(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }

func tally(s Snapshot, option Option) Result {
	out := Result{Option: option, Voters: Groups[[]uuid.UUID]{Yes: []uuid.UUID{}, Strong: []uuid.UUID{}, No: []uuid.UUID{}, Skip: []uuid.UUID{}}}
	for _, vote := range s.Votes {
		if vote.OptionID != option.ID || vote.Value == nil {
			continue
		}
		switch *vote.Value {
		case Yes:
			out.Voters.Yes = append(out.Voters.Yes, vote.PersonID)
		case Strong:
			out.Voters.Strong = append(out.Voters.Strong, vote.PersonID)
		case No:
			out.Voters.No = append(out.Voters.No, vote.PersonID)
		case Skip:
			out.Voters.Skip = append(out.Voters.Skip, vote.PersonID)
		}
	}
	for _, group := range [][]uuid.UUID{out.Voters.Yes, out.Voters.Strong, out.Voters.No, out.Voters.Skip} {
		slices.SortFunc(group, compareID)
	}
	out.Counts = Groups[int64]{Yes: int64(len(out.Voters.Yes)), Strong: int64(len(out.Voters.Strong)),
		No: int64(len(out.Voters.No)), Skip: int64(len(out.Voters.Skip))}
	out.Score = 2*out.Counts.Strong + out.Counts.Yes
	out.Matched = out.Counts.Yes+out.Counts.Strong > 0 && out.Counts.No+out.Counts.Skip == 0
	return out
}

// consistent checks that a snapshot describes one question of one tenant, and is
// what a read of a snapshot answers with ErrIntegrity instead of a tally. The
// question itself, its options, its participants and its votes are checked one
// after the other in that order, because each step needs the identities the step
// before collected: a vote is only answerable at all once the options and the
// people it can name are known to be the question's own.
func consistent(s Snapshot) error {
	if err := consistentQuestion(s.Question); err != nil {
		return err
	}
	options, err := consistentOptions(s)
	if err != nil {
		return err
	}
	people, err := consistentParticipants(s)
	if err != nil {
		return err
	}
	if !people[s.Question.CreatorID] {
		return ErrIntegrity
	}
	return consistentVotes(s, options, people)
}

// consistentQuestion checks the question itself: it exists, it belongs to a
// tenant, it carries a revision and a creator, and its status is one a question
// can be in.
func consistentQuestion(q Question) error {
	if q.ID == uuid.Nil || q.TenantID == uuid.Nil || q.Revision < 1 || q.CreatorID == uuid.Nil ||
		(q.Status != StatusOpen && q.Status != StatusClosed) {
		return ErrIntegrity
	}
	return nil
}

// consistentOptions checks every option against the question it claims to belong
// to and answers with the set of ids a vote may name.
func consistentOptions(s Snapshot) (map[uuid.UUID]bool, error) {
	options := map[uuid.UUID]bool{}
	for _, option := range s.Options {
		if option.ID == uuid.Nil || options[option.ID] || option.TenantID != s.Question.TenantID ||
			option.QuestionID != s.Question.ID || option.Position < 0 || option.Revision < 1 {
			return nil, ErrIntegrity
		}
		options[option.ID] = true
	}
	return options, nil
}

// consistentParticipants checks every participant against the question, refuses a
// person counted twice, and answers with the set of ids that may vote.
func consistentParticipants(s Snapshot) (map[uuid.UUID]bool, error) {
	people := map[uuid.UUID]bool{}
	for _, person := range s.Participants {
		if person.ID == uuid.Nil || person.PersonID == uuid.Nil || people[person.PersonID] ||
			person.TenantID != s.Question.TenantID || person.QuestionID != s.Question.ID || person.Revision != 1 {
			return nil, ErrIntegrity
		}
		people[person.PersonID] = true
	}
	return people, nil
}

// consistentVotes checks that every vote is one vote of one person on one option
// of this question, and that a vote carrying a value carries one this question's
// grammar accepts.
func consistentVotes(s Snapshot, options, people map[uuid.UUID]bool) error {
	pairs, ids := map[[2]uuid.UUID]bool{}, map[uuid.UUID]bool{}
	for _, vote := range s.Votes {
		pair := [2]uuid.UUID{vote.OptionID, vote.PersonID}
		if vote.ID == uuid.Nil || ids[vote.ID] || pairs[pair] || vote.TenantID != s.Question.TenantID ||
			vote.QuestionID != s.Question.ID || !options[vote.OptionID] || !people[vote.PersonID] ||
			vote.Revision < 1 || (vote.Value != nil && !valid(*vote.Value)) {
			return ErrIntegrity
		}
		pairs[pair], ids[vote.ID] = true, true
	}
	return nil
}
