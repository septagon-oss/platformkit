package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/modules/decide/internal"
	"gorm.io/gorm/clause"
)

type identity struct {
	TenantID uuid.UUID
	ID       uuid.UUID
	Active   bool
	Read     bool
	Manage   bool
	Vote     bool
}

type currentAccess struct{}

func (currentAccess) Current(ctx context.Context, tx db.Tx[db.Tenant], targets []uuid.UUID) (contracts.Actor, error) {
	p, _ := tenancy.PrincipalFrom(ctx)
	ids := append(slices.Clone(targets), p.UserID)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	ids = slices.Compact(ids)
	var people []identity
	if err := tx.DB().Table("decide_test_people").Clauses(clause.Locking{Strength: "SHARE"}).
		Where("tenant_id = ? AND id IN ?", db.TenantOf(tx).ID, ids).Order("id").Find(&people).Error; err != nil {
		return contracts.Actor{}, err
	}
	actor := contracts.Actor{}
	for _, person := range people {
		if !person.Active {
			return contracts.Actor{}, contracts.ErrDenied
		}
		if person.ID == p.UserID {
			actor = contracts.Actor{PersonID: person.ID, Read: person.Read, Manage: person.Manage, Vote: person.Vote}
		}
	}
	if actor.PersonID == uuid.Nil {
		return contracts.Actor{}, contracts.ErrDenied
	}
	if len(people) != len(ids) {
		return contracts.Actor{}, contracts.ErrNotFound
	}
	if p.Permissions != nil {
		allows := func(key string) bool {
			return slices.Contains(p.Permissions, key) || slices.Contains(p.Permissions, "*")
		}
		actor.Read = actor.Read && allows(contracts.PermissionRead)
		actor.Manage = actor.Manage && allows(contracts.PermissionManage)
		actor.Vote = actor.Vote && allows(contracts.PermissionVote)
	}
	return actor, nil
}

type world struct {
	admin *sql.DB
	conn  *db.Conn
	svc   *internal.Service
	clock *porttest.Clock
	ctx   context.Context
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, conn := dbtest.Schema(t, decide.Migrations)
	_, err := admin.ExecContext(t.Context(), `CREATE TABLE decide_test_people (
		tenant_id uuid NOT NULL, id uuid NOT NULL, active boolean NOT NULL,
		read boolean NOT NULL, manage boolean NOT NULL, vote boolean NOT NULL, PRIMARY KEY (tenant_id, id))`)
	if err != nil {
		t.Fatal(err)
	}
	for _, person := range []uuid.UUID{decidetest.Alice, decidetest.Bob, decidetest.Casey, decidetest.Evan} {
		if _, err := admin.ExecContext(t.Context(), `INSERT INTO decide_test_people VALUES ($1,$2,true,true,true,true)`, decidetest.Tenant, person); err != nil {
			t.Fatal(err)
		}
	}
	clock := porttest.NewClock(decidetest.At)
	svc, err := internal.New(currentAccess{}, clock.Now, contracts.InviteKey("a-secret-for-isolated-tests-only-32"), rest.RecordNoUses{}, rest.ShowNothing{}, richtext.RejectImages{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: decidetest.Tenant}), tenancy.Principal{UserID: decidetest.Alice})
	return &world{admin: admin, conn: conn, svc: svc, clock: clock, ctx: ctx}
}

func (w *world) run(t *testing.T, fn func(context.Context, db.Tx[db.Tenant]) error) {
	t.Helper()
	if err := db.Run(w.ctx, w.conn, fn); err != nil {
		t.Fatal(err)
	}
}

func put(ctx context.Context, tx db.Tx[db.Tenant], snap contracts.Snapshot) error {
	for _, table := range []string{"decide_votes", "decide_participants", "decide_options", "decide_questions", "platformkit_outbox"} {
		if err := tx.DB().Exec("DELETE FROM " + table).Error; err != nil {
			return err
		}
	}
	if err := crud.Create(ctx, tx, &snap.Question); err != nil {
		return err
	}
	for i := range snap.Options {
		if err := crud.Create(ctx, tx, &snap.Options[i]); err != nil {
			return err
		}
	}
	for i := range snap.Participants {
		if err := crud.Create(ctx, tx, &snap.Participants[i]); err != nil {
			return err
		}
	}
	for i := range snap.Votes {
		if err := crud.Create(ctx, tx, &snap.Votes[i]); err != nil {
			return err
		}
	}
	return nil
}

func loaded(tx db.Tx[db.Tenant], id uuid.UUID) (contracts.Snapshot, error) {
	q, err := crud.Get[*contracts.Question](tx, id)
	if err != nil {
		return contracts.Snapshot{}, err
	}
	out := contracts.Snapshot{Question: *q, Options: []contracts.Option{}, Participants: []contracts.Participant{}, Votes: []contracts.Vote{}}
	for _, dest := range []any{&out.Options, &out.Participants, &out.Votes} {
		if err := tx.DB().Where("question_id = ?", id).Order("id").Find(dest).Error; err != nil {
			return contracts.Snapshot{}, err
		}
	}
	return out, nil
}

func names(tx db.Tx[db.Tenant]) ([]string, error) {
	out := []string{}
	err := tx.DB().Raw(`SELECT name FROM platformkit_outbox ORDER BY created_at, id`).Scan(&out).Error
	return out, err
}

type tokenScope struct {
	Question, Option, Person uuid.UUID
	Revision                 int64
}
type trackedVoting struct {
	*internal.Service
	tokens map[tokenScope]string
}

func (s trackedVoting) Vote(ctx context.Context, tx db.Tx[db.Tenant], in contracts.VoteInput) (contracts.VoteReply, error) {
	out, err := s.Service.Vote(ctx, tx, in)
	if err == nil && out.UndoToken != "" {
		s.tokens[tokenScope{out.Vote.QuestionID, out.Vote.OptionID, out.Vote.PersonID, out.Vote.Revision}] = out.UndoToken
	}
	return out, err
}

func TestSQLBallotConformance(t *testing.T) {
	decidetest.RunVoting(t, func(t *testing.T, run func(decidetest.Fixture)) {
		w := newWorld(t)
		voting := trackedVoting{Service: w.svc, tokens: map[tokenScope]string{}}
		w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(decidetest.Fixture{Ctx: ctx, Tx: tx, Service: voting, SetTime: w.clock.Set,
				Put: func(ctx context.Context, snap contracts.Snapshot) {
					if err := put(ctx, tx, snap); err != nil {
						t.Fatal(err)
					}
				},
				Snapshot: func(_ context.Context, id uuid.UUID) (contracts.Snapshot, error) { return loaded(tx, id) },
				Published: func() []string {
					out, err := names(tx)
					if err != nil {
						t.Fatal(err)
					}
					return out
				},
				JoinProof: func(ctx context.Context, id uuid.UUID, claims contracts.InviteClaims) (bool, error) {
					snap, err := loaded(tx, id)
					if err != nil {
						return false, err
					}
					actor, err := (currentAccess{}).Current(ctx, tx, nil)
					if err != nil {
						return false, err
					}
					return contracts.AdmitJoin(snap, actor, claims, 0, w.clock.Now())
				},
				UndoProof: func(ctx context.Context, claims contracts.UndoClaims) (contracts.VoteReply, error) {
					token := voting.tokens[tokenScope{claims.QuestionID, claims.OptionID, claims.PersonID, claims.PostRevision}]
					return w.svc.UndoVote(ctx, tx, contracts.UndoVoteInput{Token: token})
				},
				Corrupt: func(ctx context.Context, snap contracts.Snapshot) (contracts.Results, error) {
					if err := tx.DB().SavePoint("bad_aggregate").Error; err != nil {
						t.Fatal(err)
					}
					err := put(ctx, tx, snap)
					if rollback := tx.DB().RollbackTo("bad_aggregate").Error; rollback != nil {
						t.Fatal(rollback)
					}
					if err == nil {
						t.Fatal("database admitted an inconsistent aggregate")
					}
					return contracts.Results{}, contracts.ErrIntegrity
				},
				OtherTenant: func(run func(context.Context, db.Tx[db.Tenant])) {
					foreign := tenancy.WithTenant(db.Detached(ctx), tenancy.Tenant{ID: uuid.New()})
					if err := db.Run(foreign, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { run(ctx, tx); return nil }); err != nil {
						t.Fatal(err)
					}
				}})
			return nil
		})
	})
}

func TestQuestionCommandsPreserveTheLastOptionAndRefuseStaleWrites(t *testing.T) {
	w := newWorld(t)
	var q contracts.Question
	var option uuid.UUID
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		q, err = w.svc.Open(ctx, tx, contracts.OpenInput{Title: "  Choose together  ", Options: []contracts.OptionInput{{Title: "First"}}})
		if err != nil {
			return err
		}
		rows, err := w.svc.Options(ctx, tx, q.ID)
		if err != nil {
			return err
		}
		option = rows[0].ID
		if q.Title != "Choose together" || q.Revision != 1 || q.CreatorID != decidetest.Alice {
			t.Fatalf("open: %+v", q)
		}
		return nil
	})
	for _, tc := range []struct {
		id       uuid.UUID
		revision int64
		want     error
	}{{option, 1, contracts.ErrLastOption}, {uuid.New(), 5, contracts.ErrStale}} {
		err := db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			before, _ := loaded(tx, q.ID)
			out, err := w.svc.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: q.ID, IfRevision: tc.revision}, OptionID: tc.id})
			after, _ := loaded(tx, q.ID)
			emitted, _ := names(tx)
			if !errors.Is(err, tc.want) || out.ID != uuid.Nil || !reflect.DeepEqual(before, after) || len(emitted) != 0 {
				t.Fatalf("refusal: %+v %v", out, err)
			}
			return err
		})
		if !errors.Is(err, tc.want) {
			t.Fatal(err)
		}
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		out, err := w.svc.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: q.ID, IfRevision: 1}, OptionID: uuid.New()})
		if err != nil || out.Revision != 1 {
			t.Fatalf("absent option: %+v %v", out, err)
		}
		changed, err := w.svc.AddOption(ctx, tx, contracts.AddOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: q.ID, IfRevision: 1}, Option: contracts.OptionInput{Title: "Second"}})
		if err != nil {
			return err
		}
		out, err = w.svc.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: q.ID, IfRevision: changed.Question.Revision}, OptionID: option})
		if err != nil || out.Revision != 3 {
			t.Fatalf("remove: %+v %v", out, err)
		}
		return nil
	})
}

func TestCommittedTenantRowsAreInvisibleToAnotherTenant(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	foreign := tenancy.WithTenant(w.ctx, tenancy.Tenant{ID: uuid.New()})
	if err := db.Run(foreign, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, table := range []string{"decide_questions", "decide_options", "decide_participants", "decide_votes"} {
			var total int64
			if err := tx.DB().Table(table).Count(&total).Error; err != nil {
				return err
			}
			if total != 0 {
				t.Fatalf("foreign %s: %d", table, total)
			}
		}
		read, err := w.svc.Results(ctx, tx, decidetest.Q)
		if !errors.Is(err, contracts.ErrNotFound) || read.QuestionID != uuid.Nil {
			t.Fatalf("foreign read: %+v %v", read, err)
		}
		out, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: contracts.Yes})
		if !errors.Is(err, contracts.ErrNotFound) || out.Vote.ID != uuid.Nil {
			t.Fatalf("foreign vote: %+v %v", out, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentBallotsSettleWithOneMatch(t *testing.T) {
	for _, sameActor := range []bool{true, false} {
		t.Run(map[bool]string{true: "one pair", false: "two voters"}[sameActor], func(t *testing.T) {
			w := newWorld(t)
			w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
			start := make(chan struct{})
			outcomes := make(chan error, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Go(func() {
					<-start
					ctx, value := w.ctx, contracts.Yes
					if i == 1 {
						value = contracts.Strong
						if !sameActor {
							ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: decidetest.Bob})
						}
					}
					outcomes <- db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						_, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: value})
						return err
					})
				})
			}
			close(start)
			wg.Wait()
			close(outcomes)
			stale := 0
			for err := range outcomes {
				if errors.Is(err, contracts.ErrStale) {
					stale++
				} else if err != nil {
					t.Fatal(err)
				}
			}
			if stale != map[bool]int{true: 1, false: 0}[sameActor] {
				t.Fatalf("stale outcomes: %d", stale)
			}
			w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				result, err := w.svc.Results(ctx, tx, decidetest.Q)
				if err != nil {
					return err
				}
				if !result.Options[0].Matched || result.Options[0].Score < 1 {
					t.Fatalf("results: %+v", result)
				}
				var total int64
				if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventMatched).Count(&total).Error; err != nil {
					return err
				}
				if total != 1 {
					t.Fatalf("matches: %d", total)
				}
				return nil
			})
		})
	}
}
