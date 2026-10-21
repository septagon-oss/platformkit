// Package internal implements locked Decide commands and aggregate reads.
package internal

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"gorm.io/gorm/clause"
)

// Service keeps collaborators explicit; callers own transactions and rollback.
type Service struct {
	access contracts.Access
	clock  contracts.Clock
	signer signer
	uses   rest.FileUses
	shown  rest.FileShown
	images richtext.Files
}

// New validates required collaborators and copies the secret before any I/O.
func New(access contracts.Access, clock contracts.Clock, key contracts.InviteKey, uses rest.FileUses, shown rest.FileShown, images richtext.Files) (*Service, error) {
	if access == nil || clock == nil || uses == nil || shown == nil || images == nil || len(key) < 32 || !slices.ContainsFunc(key, func(b byte) bool { return b != 0 }) {
		return nil, fmt.Errorf("decide: access, clock, file ports and a nonzero signing key of at least 32 bytes are required")
	}
	return &Service{access: access, clock: clock, signer: signer{slices.Clone(key)}, uses: uses, shown: shown, images: images}, nil
}

var _ contracts.Service = (*Service)(nil)

func scope(ctx context.Context, tx db.Tx[db.Tenant]) error {
	if tx.DB() == nil || db.TenantOf(tx).ID == uuid.Nil {
		return db.ErrNoTenant
	}
	if tenant, ok := tenancy.FromContext(ctx); !ok || tenant.ID != db.TenantOf(tx).ID {
		return db.ErrScopeMismatch
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock().UTC().Truncate(time.Microsecond) }

func absent(err error) error {
	if errors.Is(err, crud.ErrNotFound) {
		return contracts.ErrNotFound
	}
	return err
}

// locked takes the aggregate before identity locks. The named lock covers the
// child-row critical section, including a ballot pair which does not exist yet.
func (s *Service) locked(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, write bool, targets ...uuid.UUID) (contracts.Snapshot, contracts.Actor, error) {
	if err := scope(ctx, tx); err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, err
	}
	if id == uuid.Nil {
		return contracts.Snapshot{}, contracts.Actor{}, contracts.ErrInvalid
	}
	var q *contracts.Question
	var err error
	if write {
		q, err = crud.GetForUpdate[*contracts.Question](tx, id)
	} else {
		q = new(contracts.Question)
		err = crud.Classify(tx.DB().Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND deleted_at IS NULL", id).Take(q).Error)
	}
	if err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, absent(err)
	}
	if q.TenantID != db.TenantOf(tx).ID {
		return contracts.Snapshot{}, contracts.Actor{}, contracts.ErrNotFound
	}
	if write {
		if err := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "decide.aggregate/"+q.TenantID.String()+"/"+id.String()).Error; err != nil {
			return contracts.Snapshot{}, contracts.Actor{}, err
		}
	}
	actor, err := s.access.Current(ctx, tx, targets)
	if err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, err
	}
	out, err := snapshot(tx, *q)
	if err != nil {
		return contracts.Snapshot{}, contracts.Actor{}, err
	}
	return out, actor, nil
}

func snapshot(tx db.Tx[db.Tenant], q contracts.Question) (contracts.Snapshot, error) {
	out := contracts.Snapshot{Question: q, Options: []contracts.Option{}, Participants: []contracts.Participant{}, Votes: []contracts.Vote{}}
	for _, dest := range []any{&out.Options, &out.Participants, &out.Votes} {
		if err := tx.DB().Where("question_id = ? AND deleted_at IS NULL", q.ID).Order("id").Find(dest).Error; err != nil {
			return contracts.Snapshot{}, err
		}
	}
	utcSnapshot(&out)
	return out, nil
}

func base(tx db.Tx[db.Tenant], at time.Time) entity.Base {
	return entity.Base{ID: uuid.New(), TenantID: db.TenantOf(tx).ID, CreatedAt: at, UpdatedAt: at}
}

func creator(s contracts.Snapshot, actor contracts.Actor, in contracts.QuestionInput, at time.Time, open bool) error {
	if !actor.Read || !actor.Manage || actor.PersonID == uuid.Nil || actor.PersonID != s.Question.CreatorID {
		return contracts.ErrDenied
	}
	if in.IfRevision < 1 {
		return contracts.ErrInvalid
	}
	if in.IfRevision != s.Question.Revision {
		return contracts.ErrStale
	}
	if open && !accepting(s.Question, at) {
		return contracts.ErrClosed
	}
	return nil
}

func accepting(q contracts.Question, at time.Time) bool {
	return q.Status == contracts.StatusOpen && (q.ClosesAt == nil || at.Before(*q.ClosesAt))
}

func advance(tx db.Tx[db.Tenant], q *contracts.Question, at time.Time) error {
	if q.Revision == math.MaxInt64 {
		return contracts.ErrExhausted
	}
	q.Revision++
	q.UpdatedAt = at
	return tx.DB().Model(q).Select("*").Omit("id", "tenant_id", "created_at", "deleted_at").UpdateColumns(q).Error
}

func (s *Service) Vote(ctx context.Context, tx db.Tx[db.Tenant], in contracts.VoteInput) (contracts.VoteReply, error) {
	snap, actor, err := s.locked(ctx, tx, in.QuestionID, true)
	if err != nil {
		return contracts.VoteReply{}, err
	}
	at := s.now()
	change, err := contracts.DecideVote(snap, actor, in, uuid.New(), at)
	if err != nil {
		return contracts.VoteReply{}, err
	}
	out, err := s.ballot(ctx, tx, snap, change)
	if err != nil {
		return contracts.VoteReply{}, err
	}
	expiry := at.Add(15 * time.Minute)
	if snap.Question.ClosesAt != nil && snap.Question.ClosesAt.Before(expiry) {
		expiry = *snap.Question.ClosesAt
	}
	expiry = expiry.Truncate(time.Second)
	if change.Changed && expiry.After(at) {
		out.UndoToken = s.signer.undo(contracts.UndoClaims{TenantID: snap.Question.TenantID, QuestionID: in.QuestionID,
			OptionID: in.OptionID, PersonID: actor.PersonID, PostRevision: change.Vote.Revision, Previous: change.Previous, ExpiresAt: expiry})
	}
	return out, nil
}

func (s *Service) UndoVote(ctx context.Context, tx db.Tx[db.Tenant], in contracts.UndoVoteInput) (contracts.VoteReply, error) {
	claims, err := s.signer.readUndo(in.Token)
	if err != nil {
		return contracts.VoteReply{}, err
	}
	if err := scope(ctx, tx); err != nil {
		return contracts.VoteReply{}, err
	}
	if claims.TenantID != db.TenantOf(tx).ID {
		return contracts.VoteReply{}, contracts.ErrUndo
	}
	snap, actor, err := s.locked(ctx, tx, claims.QuestionID, true)
	if err != nil {
		return contracts.VoteReply{}, err
	}
	change, err := contracts.DecideUndo(snap, actor, claims, s.now())
	if err != nil {
		return contracts.VoteReply{}, err
	}
	return s.ballot(ctx, tx, snap, change)
}

func (s *Service) ballot(ctx context.Context, tx db.Tx[db.Tenant], snap contracts.Snapshot, change contracts.BallotChange) (contracts.VoteReply, error) {
	if !change.Changed {
		return contracts.VoteReply{Vote: change.Vote}, nil
	}
	v := change.Vote
	var err error
	if v.Revision == 1 {
		err = crud.Create(ctx, tx, &v)
	} else {
		err = tx.DB().Model(&v).Select("value", "revision", "at", "updated_at").UpdateColumns(&v).Error
	}
	if err != nil {
		return contracts.VoteReply{}, err
	}
	if err := events.Succeed(ctx, tx, v.At); err != nil {
		return contracts.VoteReply{}, err
	}
	if err := events.Publish(ctx, tx, contracts.EventVoted, contracts.Voted{QuestionID: v.QuestionID, OptionID: v.OptionID, PersonID: v.PersonID,
		Previous: change.Previous, Value: v.Value, VoteRevision: v.Revision, At: v.At}); err != nil {
		return contracts.VoteReply{}, err
	}
	if change.FirstMatch {
		if err := tx.DB().Model(&contracts.Option{}).Where("id = ?", v.OptionID).UpdateColumn("first_matched_at", v.At).Error; err != nil {
			return contracts.VoteReply{}, err
		}
		snap.Votes = slices.DeleteFunc(snap.Votes, func(old contracts.Vote) bool { return old.OptionID == v.OptionID && old.PersonID == v.PersonID })
		snap.Votes = append(snap.Votes, v)
		ranked, err := contracts.Rank(snap)
		if err != nil {
			return contracts.VoteReply{}, err
		}
		payload := contracts.Matched{QuestionID: v.QuestionID, OptionID: v.OptionID, At: v.At, Recipients: []uuid.UUID{}, Voters: []uuid.UUID{}}
		for _, result := range ranked {
			if result.Option.ID == v.OptionID {
				payload.Voters = append(payload.Voters, result.Voters.Yes...)
				payload.Voters = append(payload.Voters, result.Voters.Strong...)
			}
		}
		for _, person := range snap.Participants {
			payload.Recipients = append(payload.Recipients, person.PersonID)
		}
		slices.SortFunc(payload.Voters, compareID)
		slices.SortFunc(payload.Recipients, compareID)
		if err := events.Publish(ctx, tx, contracts.EventMatched, payload); err != nil {
			return contracts.VoteReply{}, err
		}
	}
	return contracts.VoteReply{Vote: v}, nil
}

func compareID(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) }
