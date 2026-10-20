package decidetest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

// Fixture supplies a transaction-shaped world. A SQL harness seeds the same
// snapshots and reads its real outbox; the fake uses existing porttest units.
type Fixture struct {
	Ctx         context.Context
	Tx          db.Tx[db.Tenant]
	Service     Voting
	Put         func(context.Context, contracts.Snapshot)
	Snapshot    func(context.Context, uuid.UUID) (contracts.Snapshot, error)
	Published   func() []string
	SetTime     func(time.Time)
	JoinProof   func(context.Context, uuid.UUID, contracts.InviteClaims) (bool, error)
	UndoProof   func(context.Context, contracts.UndoClaims) (contracts.VoteReply, error)
	OtherTenant func(func(context.Context, db.Tx[db.Tenant]))
}

// Harness owns one isolated fixture's lifetime and calls the case inside it.
type Harness func(*testing.T, func(Fixture))

// RunVoting checks ballot, read and verified-admission decisions. It does not
// claim full Service, SQL concurrency, token signing or served-deck conformance.
func RunVoting(t *testing.T, h Harness) {
	t.Helper()
	porttest.Run(t, porttest.Suite[Fixture]{Port: "decide ballot decisions", World: h,
		Events: func(f Fixture) []string { return f.Published() }, Own: cases()})
}

func cases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{Name: "one ballot per pair changes without duplicate retry events", Because: "Checks a sequence of two distinct values and both replay forms.", Run: uniqueBallot},
		{Name: "rewind restores absence without recycling a ballot revision", Because: "A retained tombstone prevents a stale token from undoing a later cast.", Run: rewind},
		{Name: "match is nonempty actual voter agreement and emits once", Because: "Live truth can change while the lifetime event latch cannot.", Run: agreement},
		{Name: "ranking counts groups and deterministic ties agree", Because: "Several actors and options establish an independent score and identity oracle.", Run: ranking},
		{Name: "refused ballots return zero and preserve state and events", Because: "Each refusal must witness the entire aggregate and event recorder.", Run: refusals},
		{Name: "another tenant cannot read or change a question", Because: "The owner's snapshot is checked after calls from another tenant.", Run: tenantScope},
		{Name: "deck is participant specific and skip leaves its queue", Because: "Queue/progress are actor projections, not resource literal filters.", Run: queue},
		{Name: "deadline equality refuses ballots and empties the deck", Because: "Admission must stop before a later job materializes closed state.", Run: deadline},
		{Name: "verified link admission checks current enabling epoch expiry and scope", Because: "MAC verification and SQL insertion are separate obligations from these domain decisions.", Run: admission},
		{Name: "removed choices and null ballots do not affect results", Because: "Retained history and active choices have different read membership.", Run: retained},
		{Name: "read outputs cannot mutate the stored aggregate", Because: "Nested pointers and slices require a detached fake store.", Run: detached},
		{Name: "inconsistent ballot aggregates refuse partial tally", Because: "A corrupted duplicate or cross-question row must not inflate a score.", Run: integrity},
	}
}

var (
	Tenant = id(1)
	Alice  = id(2)
	Bob    = id(3)
	Casey  = id(4)
	Evan   = id(5)
	Q      = id(10)
	A      = id(20)
	B      = id(21)
	C      = id(22)
	At     = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
)

func id(n byte) uuid.UUID { var out uuid.UUID; out[15] = n; return out }

// Seed is a complete open question with three options and three participants.
// Its UUIDs and time are stable so rank and voter order assertions are independent.
func Seed() contracts.Snapshot {
	base := func(n uuid.UUID) entity.Base {
		return entity.Base{ID: n, TenantID: Tenant, CreatedAt: At, UpdatedAt: At}
	}
	s := contracts.Snapshot{Question: contracts.Question{Base: base(Q), Title: "Choose", Status: contracts.StatusOpen, CreatorID: Alice, Revision: 1}}
	for i, option := range []uuid.UUID{A, B, C} {
		s.Options = append(s.Options, contracts.Option{Base: base(option), QuestionID: Q, Title: option.String(), Position: int64(2 - min(i, 1)), Revision: 1})
	}
	for i, person := range []uuid.UUID{Alice, Bob, Casey} {
		s.Participants = append(s.Participants, contracts.Participant{Base: base(id(byte(30 + i))), QuestionID: Q, PersonID: person, JoinedAt: At, Revision: 1})
	}
	return s
}

func as(ctx context.Context, person uuid.UUID) context.Context {
	return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person})
}

func cast(t *testing.T, f Fixture, person, option uuid.UUID, value contracts.Value, revision int64) contracts.VoteReply {
	t.Helper()
	out, err := f.Service.Vote(as(f.Ctx, person), f.Tx, contracts.VoteInput{QuestionID: Q, OptionID: option, Value: value, IfRevision: revision})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func snapshot(t *testing.T, f Fixture) contracts.Snapshot {
	t.Helper()
	out, err := f.Snapshot(f.Ctx, Q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func results(t *testing.T, f Fixture) contracts.Results {
	t.Helper()
	out, err := f.Service.Results(as(f.Ctx, Alice), f.Tx, Q)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func result(t *testing.T, f Fixture, option uuid.UUID) contracts.Result {
	t.Helper()
	for _, row := range results(t, f).Options {
		if row.Option.ID == option {
			return row
		}
	}
	t.Fatal("missing option")
	return contracts.Result{}
}

func uniqueBallot(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	first := cast(t, f, Alice, A, contracts.Yes, 0)
	if first.Vote.Revision != 1 || !first.Vote.At.Equal(At) {
		t.Fatalf("first ballot: %+v", first)
	}
	before := snapshot(t, f)
	events := slices.Clone(f.Published())
	for _, revision := range []int64{0, 1} {
		got := cast(t, f, Alice, A, contracts.Yes, revision)
		if !reflect.DeepEqual(got.Vote, first.Vote) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(events, f.Published()) {
			t.Fatal("same-value replay changed ballot, time, state or events")
		}
	}
	f.SetTime(At.Add(time.Minute))
	changed := cast(t, f, Alice, A, contracts.Strong, 1)
	s := snapshot(t, f)
	if len(s.Votes) != 1 || changed.Vote.ID != first.Vote.ID || changed.Vote.Revision != 2 || !changed.Vote.At.Equal(At.Add(time.Minute)) || result(t, f, A).Score != 2 {
		t.Fatalf("upsert duplicated or failed to change: %+v", s.Votes)
	}
}

func rewind(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	cast(t, f, Alice, A, contracts.Yes, 0)
	claims := contracts.UndoClaims{TenantID: Tenant, QuestionID: Q, OptionID: A, PersonID: Alice, PostRevision: 1, ExpiresAt: At.Add(15 * time.Minute)}
	f.SetTime(At.Add(time.Minute))
	undone, err := f.UndoProof(as(f.Ctx, Alice), claims)
	if err != nil || undone.Vote.Value != nil || undone.Vote.Revision != 2 || !undone.Vote.At.Equal(At.Add(time.Minute)) || result(t, f, A).Matched {
		t.Fatalf("rewind failed to restore absence: %+v %v", undone, err)
	}
	before, emitted := snapshot(t, f), slices.Clone(f.Published())
	replayed, err := f.UndoProof(as(f.Ctx, Alice), claims)
	if err != nil || !reflect.DeepEqual(replayed, undone) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("rewind replay changed state or events")
	}
	deck, err := f.Service.Deck(as(f.Ctx, Alice), f.Tx, Q)
	if err != nil || deck.Remaining != 3 || deck.Voted != 0 {
		t.Fatal("rewind failed to return the option to the deck")
	}
	cast(t, f, Alice, A, contracts.Strong, 2)
	before, emitted = snapshot(t, f), slices.Clone(f.Published())
	refused, err := f.UndoProof(as(f.Ctx, Alice), claims)
	if !errors.Is(err, contracts.ErrStale) || !reflect.DeepEqual(refused, contracts.VoteReply{}) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("old rewind token survived a later cast")
	}
	if before.Options[0].FirstMatchedAt == nil {
		t.Fatal("rewind reset the lifetime match latch")
	}
}

func agreement(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	if result(t, f, A).Matched {
		t.Fatal("zero voters matched")
	}
	cast(t, f, Alice, A, contracts.Yes, 0)
	if !result(t, f, A).Matched {
		t.Fatal("nonvoters vetoed one affirmative voter")
	}
	cast(t, f, Bob, A, contracts.Skip, 0)
	if result(t, f, A).Matched {
		t.Fatal("skip did not veto agreement")
	}
	cast(t, f, Bob, A, contracts.No, 1)
	if result(t, f, A).Matched {
		t.Fatal("no did not veto agreement")
	}
	cast(t, f, Bob, A, contracts.Strong, 2)
	if !result(t, f, A).Matched {
		t.Fatal("yes and strong failed to match")
	}
	want := []string{contracts.EventVoted, contracts.EventMatched, contracts.EventVoted, contracts.EventVoted, contracts.EventVoted}
	if !slices.Equal(f.Published(), want) {
		t.Fatalf("events: %v", f.Published())
	}
}

func ranking(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	cast(t, f, Bob, A, contracts.Strong, 0)
	cast(t, f, Alice, A, contracts.Yes, 0)
	cast(t, f, Bob, B, contracts.Yes, 0)
	cast(t, f, Alice, B, contracts.Yes, 0)
	cast(t, f, Alice, C, contracts.No, 0)
	rows := results(t, f).Options
	if len(rows) != 3 || rows[0].Option.ID != A || rows[0].Score != 3 || rows[1].Option.ID != B || rows[1].Score != 2 || rows[2].Score != 0 {
		t.Fatalf("ranking: %+v", rows)
	}
	if rows[0].Counts != (contracts.Groups[int64]{Yes: 1, Strong: 1}) || !slices.Equal(rows[0].Voters.Yes, []uuid.UUID{Alice}) || !slices.Equal(rows[0].Voters.Strong, []uuid.UUID{Bob}) || !slices.Equal(rows[1].Voters.Yes, []uuid.UUID{Alice, Bob}) || rows[0].Voters.No == nil || rows[0].Voters.Skip == nil {
		t.Fatalf("counts/voters: %+v", rows)
	}
	f.Put(f.Ctx, Seed())
	rows = results(t, f).Options
	if rows[0].Option.ID != B || rows[1].Option.ID != C || rows[2].Option.ID != A {
		t.Fatalf("ties ignored position or ID: %+v", rows)
	}
}

func refusals(t *testing.T, f Fixture) {
	for _, tc := range []struct {
		name     string
		person   uuid.UUID
		option   uuid.UUID
		value    contracts.Value
		revision int64
		want     error
	}{
		{"nonparticipant", Evan, A, contracts.Yes, 0, contracts.ErrDenied},
		{"unknown option", Alice, id(99), contracts.Yes, 0, contracts.ErrNotFound},
		{"invalid value", Alice, A, "maybe", 0, contracts.ErrInvalid},
		{"negative revision", Alice, A, contracts.Yes, -1, contracts.ErrInvalid},
		{"stale absent revision", Alice, A, contracts.Yes, 5, contracts.ErrStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.Put(f.Ctx, Seed())
			before, emitted := snapshot(t, f), slices.Clone(f.Published())
			out, err := f.Service.Vote(as(f.Ctx, tc.person), f.Tx, contracts.VoteInput{QuestionID: Q, OptionID: tc.option, Value: tc.value, IfRevision: tc.revision})
			if !errors.Is(err, tc.want) || !reflect.DeepEqual(out, contracts.VoteReply{}) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
				t.Fatalf("refusal leaked or changed data: %+v %v", out, err)
			}
		})
	}
	f.Put(f.Ctx, Seed())
	cast(t, f, Alice, A, contracts.Yes, 0)
	cast(t, f, Alice, A, contracts.No, 1)
	cast(t, f, Alice, A, contracts.Yes, 2)
	before, emitted := snapshot(t, f), slices.Clone(f.Published())
	out, err := f.Service.Vote(as(f.Ctx, Alice), f.Tx, contracts.VoteInput{QuestionID: Q, OptionID: A, Value: contracts.Yes, IfRevision: 0})
	if !errors.Is(err, contracts.ErrStale) || !reflect.DeepEqual(out, contracts.VoteReply{}) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("an old same-value command survived intervening changes")
	}
	ctx := tenancy.WithPrincipal(f.Ctx, tenancy.Principal{UserID: Alice, Permissions: []string{contracts.PermissionRead}})
	out, err = f.Service.Vote(ctx, f.Tx, contracts.VoteInput{QuestionID: Q, OptionID: A, Value: contracts.Yes, IfRevision: 3})
	if !errors.Is(err, contracts.ErrDenied) || !reflect.DeepEqual(out, contracts.VoteReply{}) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("credential ceiling was not rechecked on a no-op")
	}
}

func tenantScope(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	before, emitted := snapshot(t, f), slices.Clone(f.Published())
	f.OtherTenant(func(ctx context.Context, tx db.Tx[db.Tenant]) {
		foreign := as(ctx, Alice)
		out, err := f.Service.Vote(foreign, tx, contracts.VoteInput{QuestionID: Q, OptionID: A, Value: contracts.Yes})
		if !errors.Is(err, contracts.ErrNotFound) || !reflect.DeepEqual(out, contracts.VoteReply{}) {
			t.Fatalf("foreign mutation: %+v %v", out, err)
		}
		read, err := f.Service.Results(foreign, tx, Q)
		if !errors.Is(err, contracts.ErrNotFound) || !reflect.DeepEqual(read, contracts.Results{}) {
			t.Fatal("foreign caller read owner's results")
		}
	})
	if !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("foreign caller reached or changed owner's aggregate")
	}
}

func queue(t *testing.T, f Fixture) {
	f.Put(f.Ctx, Seed())
	cast(t, f, Alice, A, contracts.Skip, 0)
	deck, err := f.Service.Deck(as(f.Ctx, Alice), f.Tx, Q)
	if err != nil || deck.Total != 3 || deck.Voted != 1 || deck.Remaining != 2 || len(deck.Cards) != 2 || deck.Cards[0].Option.ID != B || deck.Cards[1].Option.ID != C {
		t.Fatalf("actor queue: %+v %v", deck, err)
	}
	other, err := f.Service.Deck(as(f.Ctx, Bob), f.Tx, Q)
	if err != nil || other.Remaining != 3 || other.Voted != 0 {
		t.Fatalf("other actor inherited queue: %+v %v", other, err)
	}
	denied, err := f.Service.Deck(as(f.Ctx, Evan), f.Tx, Q)
	if !errors.Is(err, contracts.ErrDenied) || !reflect.DeepEqual(denied, contracts.Deck{}) {
		t.Fatal("nonparticipant reached queue")
	}
}

func deadline(t *testing.T, f Fixture) {
	s := Seed()
	s.Question.ClosesAt = new(At)
	f.Put(f.Ctx, s)
	before, emitted := snapshot(t, f), slices.Clone(f.Published())
	out, err := f.Service.Vote(as(f.Ctx, Alice), f.Tx, contracts.VoteInput{QuestionID: Q, OptionID: A, Value: contracts.Yes})
	if !errors.Is(err, contracts.ErrClosed) || !reflect.DeepEqual(out, contracts.VoteReply{}) || !reflect.DeepEqual(before, snapshot(t, f)) || !reflect.DeepEqual(emitted, f.Published()) {
		t.Fatal("deadline did not refuse silently at equality")
	}
	deck, err := f.Service.Deck(as(f.Ctx, Alice), f.Tx, Q)
	if err != nil || deck.AcceptingVotes || deck.Total != 3 || len(deck.Cards) != 0 || results(t, f).AcceptingVotes {
		t.Fatal("deadline read still offered votes")
	}
}

func admission(t *testing.T, f Fixture) {
	s := Seed()
	s.Question.InviteGeneration, s.Question.InviteExpiresAt = 1, new(At.Add(time.Hour))
	claims := contracts.InviteClaims{TenantID: Tenant, QuestionID: Q, Generation: 1, ExpiresAt: At.Add(time.Hour)}
	f.Put(f.Ctx, s)
	if add, err := f.JoinProof(as(f.Ctx, Evan), Q, claims); add || !errors.Is(err, contracts.ErrJoinDisabled) {
		t.Fatalf("disabled join: %v %v", add, err)
	}
	s.Question.AllowLinkJoin = true
	f.Put(f.Ctx, s)
	if add, err := f.JoinProof(as(f.Ctx, Evan), Q, claims); !add || err != nil {
		t.Fatalf("valid new participant refused: %v %v", add, err)
	}
	if add, err := f.JoinProof(as(f.Ctx, Alice), Q, claims); add || err != nil {
		t.Fatalf("existing participant was not a no-op: %v %v", add, err)
	}
	for _, wrong := range []contracts.InviteClaims{
		{TenantID: id(91), QuestionID: Q, Generation: 1, ExpiresAt: claims.ExpiresAt},
		{TenantID: Tenant, QuestionID: id(92), Generation: 1, ExpiresAt: claims.ExpiresAt},
		{TenantID: Tenant, QuestionID: Q, Generation: 2, ExpiresAt: claims.ExpiresAt},
		{TenantID: Tenant, QuestionID: Q, Generation: 1, ExpiresAt: At.Add(2 * time.Hour)},
	} {
		if add, err := f.JoinProof(as(f.Ctx, Evan), Q, wrong); add || !errors.Is(err, contracts.ErrInvite) {
			t.Fatalf("wrong credential accepted: %v %v", add, err)
		}
	}
	f.SetTime(claims.ExpiresAt)
	if add, err := f.JoinProof(as(f.Ctx, Evan), Q, claims); add || !errors.Is(err, contracts.ErrInvite) {
		t.Fatal("expired link accepted at equality")
	}
}

func retained(t *testing.T, f Fixture) {
	s := Seed()
	s.Options[0].RemovedAt = new(At)
	s.Votes = []contracts.Vote{{Base: entity.Base{ID: id(60), TenantID: Tenant}, QuestionID: Q, OptionID: A, PersonID: Alice, Value: new(contracts.Strong), Revision: 1, At: At},
		{Base: entity.Base{ID: id(61), TenantID: Tenant}, QuestionID: Q, OptionID: B, PersonID: Alice, Revision: 2, At: At}}
	f.Put(f.Ctx, s)
	rows := results(t, f).Options
	if len(rows) != 2 || rows[0].Score != 0 || rows[0].Matched || rows[1].Matched {
		t.Fatalf("retained ballots counted: %+v", rows)
	}
	deck, err := f.Service.Deck(as(f.Ctx, Alice), f.Tx, Q)
	if err != nil || deck.Total != 2 || deck.Voted != 0 || deck.Cards[0].Option.ID != B || deck.Cards[0].VoteRevision != 2 {
		t.Fatalf("tombstone lost revision: %+v %v", deck, err)
	}
	cast(t, f, Alice, B, contracts.Yes, 2)
	if result(t, f, B).Score != 1 {
		t.Fatal("cast after undo failed")
	}
}

func detached(t *testing.T, f Fixture) {
	s := Seed()
	s.Options[0].ImageID = new(id(100))
	f.Put(f.Ctx, s)
	before := snapshot(t, f)
	rows := results(t, f).Options
	for i := range rows {
		rows[i].Option.Title = "changed"
		if rows[i].Option.ImageID != nil {
			*rows[i].Option.ImageID = id(101)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, f)) {
		t.Fatal("result alias changed stored nested data")
	}
}

func integrity(t *testing.T, f Fixture) {
	base := Seed()
	for _, corrupt := range []func(*contracts.Snapshot){
		func(s *contracts.Snapshot) { s.Options = append(s.Options, s.Options[0]) },
		func(s *contracts.Snapshot) { s.Options[0].QuestionID = id(101) },
		func(s *contracts.Snapshot) { s.Participants = append(s.Participants, s.Participants[0]) },
	} {
		s := base
		s.Options, s.Participants = slices.Clone(base.Options), slices.Clone(base.Participants)
		corrupt(&s)
		f.Put(f.Ctx, s)
		out, err := f.Service.Results(as(f.Ctx, Alice), f.Tx, Q)
		if !errors.Is(err, contracts.ErrIntegrity) || !reflect.DeepEqual(out, contracts.Results{}) {
			t.Fatalf("partial corrupt tally: %+v %v", out, err)
		}
	}
}
