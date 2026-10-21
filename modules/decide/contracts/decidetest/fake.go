// Package decidetest supplies executable ballot conformance and a decision fake.
// The fake delegates the shared rules and reuses porttest plumbing. It does not
// implement SQL, credential signing, the full Service, rollback or RLS.
package decidetest

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
)

// Voting is the service subset whose deterministic decisions are specified now.
// The SQL Service must pass RunVoting; its transaction/concurrency cases are
// additional obligations, never conclusions drawn from this fake.
type Voting interface {
	Vote(context.Context, db.Tx[db.Tenant], contracts.VoteInput) (contracts.VoteReply, error)
	Results(context.Context, db.Tx[db.Tenant], uuid.UUID) (contracts.Results, error)
	Deck(context.Context, db.Tx[db.Tenant], uuid.UUID) (contracts.Deck, error)
}

// Fake stores complete question snapshots in the kernel's tenant-partitioned
// Store. Tx is ignored; context tenancy is the fake's explicit, limited model.
type Fake struct {
	*porttest.Fake
	rows *porttest.Store[contracts.Snapshot]
}

// NewFake starts empty at the supplied time, with no actor grants.
func NewFake(at time.Time) *Fake {
	return &Fake{Fake: porttest.NewFake(at, []string{contracts.EventVoted, contracts.EventMatched, contracts.EventClosed}),
		rows: porttest.NewStore(func(s contracts.Snapshot) uuid.UUID { return s.Question.ID })}
}

var _ Voting = (*Fake)(nil)

// Put seeds a complete aggregate. It does not invent entity or identity rules.
func (f *Fake) Put(ctx context.Context, s contracts.Snapshot) { f.rows.Put(ctx, s) }

// Snapshot returns a detached view for refusal witnesses and integrity cases.
func (f *Fake) Snapshot(ctx context.Context, id uuid.UUID) (contracts.Snapshot, error) {
	s, err := f.rows.Get(ctx, id)
	return s, absent(err)
}

// Vote delegates the shared decision before porttest.Do changes any state.
// Rewind token issuance is deliberately outside this decision fake's claims.
func (f *Fake) Vote(ctx context.Context, _ db.Tx[db.Tenant], in contracts.VoteInput) (contracts.VoteReply, error) {
	return f.change(ctx, in.QuestionID, func(s contracts.Snapshot) (contracts.BallotChange, error) {
		return contracts.DecideVote(s, f.actor(ctx), in, uuid.New(), f.Clock.Now())
	})
}

// UndoVerified delegates rewind after MAC verification, without minting tokens.
func (f *Fake) UndoVerified(ctx context.Context, claims contracts.UndoClaims) (contracts.VoteReply, error) {
	return f.change(ctx, claims.QuestionID, func(s contracts.Snapshot) (contracts.BallotChange, error) {
		return contracts.DecideUndo(s, f.actor(ctx), claims, f.Clock.Now())
	})
}

func (f *Fake) change(ctx context.Context, question uuid.UUID, decide func(contracts.Snapshot) (contracts.BallotChange, error)) (contracts.VoteReply, error) {
	var change contracts.BallotChange
	_, err := porttest.Do(ctx, f.Fake, f.rows, porttest.Command[contracts.Snapshot]{
		Row: question, Permission: contracts.PermissionVote, Denied: contracts.ErrDenied,
		Decide: func(s contracts.Snapshot) error {
			var err error
			change, err = decide(s)
			return err
		},
		Apply: func(s *contracts.Snapshot) []string {
			if !change.Changed {
				return nil
			}
			s.Votes = slices.DeleteFunc(s.Votes, func(v contracts.Vote) bool {
				return v.OptionID == change.Vote.OptionID && v.PersonID == change.Vote.PersonID
			})
			s.Votes = append(s.Votes, change.Vote)
			names := []string{contracts.EventVoted}
			if change.FirstMatch {
				for i := range s.Options {
					if s.Options[i].ID == change.Vote.OptionID {
						s.Options[i].FirstMatchedAt = new(change.Vote.At)
					}
				}
				names = append(names, contracts.EventMatched)
			}
			return names
		},
	})
	if err != nil {
		return contracts.VoteReply{}, absent(err)
	}
	return contracts.VoteReply{Vote: change.Vote}, nil
}

// Results delegates current scope and ranking decisions over detached storage.
func (f *Fake) Results(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (contracts.Results, error) {
	s, err := f.Snapshot(ctx, id)
	if err != nil {
		return contracts.Results{}, err
	}
	return contracts.ReadResults(s, f.actor(ctx), f.Clock.Now())
}

// Deck delegates queue/progress and membership decisions for the current actor.
func (f *Fake) Deck(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (contracts.Deck, error) {
	s, err := f.Snapshot(ctx, id)
	if err != nil {
		return contracts.Deck{}, err
	}
	return contracts.ReadDeck(s, f.actor(ctx), f.Clock.Now())
}

// JoinVerified models only admission after MAC verification. It mutates nothing;
// the full Join command must additionally prove signing and membership atomicity.
func (f *Fake) JoinVerified(ctx context.Context, id uuid.UUID, claims contracts.InviteClaims) (bool, error) {
	s, err := f.Snapshot(ctx, id)
	if err != nil {
		return false, err
	}
	return contracts.AdmitJoin(s, f.actor(ctx), claims, 0, f.Clock.Now())
}

func (f *Fake) actor(ctx context.Context) contracts.Actor {
	holds := func(key string) bool {
		if p, ok := tenancy.PrincipalFrom(ctx); ok && p.Permissions != nil &&
			!slices.Contains(p.Permissions, key) && !slices.Contains(p.Permissions, "*") {
			return false
		}
		return f.Grants.Holds(ctx, key)
	}
	return contracts.Actor{PersonID: porttest.Actor(ctx), Read: holds(contracts.PermissionRead),
		Manage: holds(contracts.PermissionManage), Vote: holds(contracts.PermissionVote)}
}

func absent(err error) error {
	if errors.Is(err, crud.ErrNotFound) {
		return contracts.ErrNotFound
	}
	return err
}
