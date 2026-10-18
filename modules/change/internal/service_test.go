package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
var other = tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}

// The two people a proposal needs. One account could not exercise the rule that
// needs two, so the test has both and the service decides which one it was given.
var (
	proposer = uuid.MustParse("01990000-0000-7000-8000-0000000000a1")
	reviewer = uuid.MustParse("01990000-0000-7000-8000-0000000000b2")
)

// counter is the subject these tests propose against: one row of JSON with a
// revision, which is the whole of what contracts.Subject asks a subject to be.
type counter struct {
	current  json.RawMessage
	revision int64
	// moved simulates the subject being written by somebody else, which is the only
	// way staleness can be real in a test.
	moved bool
	saved []byte
	saves int
}

func (c *counter) Lock(context.Context, db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	if c.moved {
		c.revision++
		c.moved = false
	}
	return c.current, c.revision, nil
}

func (c *counter) Save(_ context.Context, _ db.Tx[db.Tenant], merged json.RawMessage) (int64, error) {
	c.saves++
	c.saved = merged
	c.revision++
	return c.revision, nil
}

func bindingTo(subject *counter) []contracts.SubjectBinding {
	return []contracts.SubjectBinding{{
		Module: "counter", Entity: "one",
		Resolve: func(context.Context, db.Tx[db.Tenant], uuid.UUID) (contracts.Subject, error) {
			return subject, nil
		},
	}}
}

func newCounter() *counter {
	return &counter{current: json.RawMessage(`{"label":"before","limit":10}`), revision: 7}
}

func diff(t *testing.T, text string) contracts.Diff {
	t.Helper()
	var d contracts.Diff
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		t.Fatalf("the test's own diff: %v", err)
	}
	return d
}

func propose(t *testing.T, subject *counter, svc contracts.Service, ctx context.Context, tx db.Tx[db.Tenant]) *contracts.Proposal {
	t.Helper()
	row, err := svc.Propose(ctx, tx, contracts.NewProposal{
		SubjectModule: "counter", SubjectEntity: "one",
		Diff: diff(t, `{"limit":11}`), Summary: "raise the limit by one",
	})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	return row
}

// TestLifecycle runs the one path a proposal can be decided by: proposed, approved
// by somebody else, applied once, with the subject written by the apply and each
// transition publishing exactly one event in the caller's own transaction.
func TestLifecycle(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		row := propose(t, subject, svc, ctx, tx)
		if row.State != contracts.StateProposed || row.Revision != 1 || row.BaseRevision != 7 {
			t.Fatalf("a new proposal: %+v", row)
		}
		if row.Proposer != proposer {
			t.Errorf("the proposer is %s, taken from the credentials, not from the body", row.Proposer)
		}
		if got := published(t, tx); len(got) != 1 || got[0] != contracts.EventProposed {
			t.Fatalf("proposing published %v", got)
		}

		ctx = tenancy.WithActor(ctx, reviewer)
		approved, err := svc.Review(ctx, tx, row.ID, contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 1})
		if err != nil {
			t.Fatalf("review: %v", err)
		}
		if approved.State != contracts.StateApproved || approved.Revision != 2 {
			t.Fatalf("an approved proposal: %+v", approved)
		}
		if approved.Reviewer == nil || *approved.Reviewer != reviewer {
			t.Errorf("the reviewer on the row is %v, and it is the actor rather than a body field", approved.Reviewer)
		}

		applied, err := svc.Apply(ctx, tx, row.ID, approved.Revision)
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if applied.State != contracts.StateApplied || applied.AppliedRevision != 8 {
			t.Fatalf("an applied proposal: %+v", applied)
		}
		if subject.saves != 1 {
			t.Errorf("the subject was written %d times, and one apply is one write", subject.saves)
		}
		var merged map[string]any
		if err := json.Unmarshal(subject.saved, &merged); err != nil {
			t.Fatalf("what the apply wrote: %v", err)
		}
		if merged["label"] != "before" || merged["limit"].(float64) != 11 {
			t.Errorf("the merge produced %s, and RFC 7386 replaces one key", subject.saved)
		}

		// A retry of an applied proposal is the same row and no second write: the
		// thing that was approved was one write, not however many asks arrive.
		again, err := svc.Apply(ctx, tx, row.ID, applied.Revision)
		if err != nil {
			t.Fatalf("replaying an apply: %v", err)
		}
		if again.State != contracts.StateApplied || again.Revision != applied.Revision {
			t.Errorf("a replayed apply moved the row: %+v", again)
		}
		if subject.saves != 1 {
			t.Errorf("a replay wrote the subject %d times", subject.saves)
		}
		if got := published(t, tx); len(got) != 3 {
			t.Errorf("the lifecycle published %v, and each transition is one event", got)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

// TestRefusalsWriteNothingAndPublishNothing is the whole of the four-eyes rule and
// the optimistic revision, each refusal silent, each leaving the row where it was.
func TestRefusalsWriteNothingAndPublishNothing(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, svc contracts.Service, ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Proposal) error
		// want is the sentinel the refusal has to be; nil means crud.ErrConflict,
		// which is what a refusal with nothing specific to say about the row is.
		want   error
		events int
	}{
		{
			name: "the proposer may not review",
			run: func(t *testing.T, svc contracts.Service, ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Proposal) error {
				_, err := svc.Review(ctx, tx, row.ID, contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 1})
				return err
			},
			want:   contracts.ErrSelfReview,
			events: 1,
		},
		{
			name: "the revision must be the one the decider read",
			run: func(t *testing.T, svc contracts.Service, ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Proposal) error {
				ctx = tenancy.WithActor(ctx, reviewer)
				_, err := svc.Review(ctx, tx, row.ID, contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 40})
				return err
			},
			want:   crud.ErrConflict,
			events: 1,
		},
		{
			name: "a proposal only the proposer may withdraw",
			run: func(t *testing.T, svc contracts.Service, ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Proposal) error {
				ctx = tenancy.WithActor(ctx, reviewer)
				_, err := svc.Withdraw(ctx, tx, row.ID, row.Revision)
				return err
			},
			want:   crud.ErrConflict,
			events: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, change.Migrations)
			subject := newCounter()
			svc := internal.NewService(bindingTo(subject))
			err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				ctx = tenancy.WithActor(ctx, proposer)
				row := propose(t, subject, svc, ctx, tx)
				before := *row
				err := tc.run(t, svc, ctx, tx, row)
				if err == nil {
					return errors.New(tc.name + ": the refusal never came")
				}
				if tc.want != nil && !errors.Is(err, tc.want) {
					return errors.New(tc.name + ": " + err.Error())
				}
				if tc.want == nil && !errors.Is(err, crud.ErrConflict) {
					return errors.New(tc.name + ": " + err.Error())
				}
				if subject.saves != 0 {
					return errors.New(tc.name + ": a refusal wrote the subject")
				}
				if got := published(t, tx); len(got) != tc.events {
					return errors.New(tc.name + ": published " + got[len(got)-1])
				}
				after, getErr := svc.Get(ctx, tx, before.ID)
				if getErr != nil {
					return getErr
				}
				if after.State != contracts.StateProposed || after.Revision != 1 {
					return errors.New(tc.name + ": the row moved anyway")
				}
				return errRollback
			})
			if !errors.Is(err, errRollback) {
				t.Fatal(err)
			}
		})
	}
}

// TestStaleBaseRefusesTheWrite is the difference between "reviewed against an exact
// revision" and a comment: the subject moved, so the approved diff is not the
// change anybody approved, and nothing is written.
func TestStaleBaseRefusesTheWrite(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		row := propose(t, subject, svc, ctx, tx)
		ctx = tenancy.WithActor(ctx, reviewer)
		approved, err := svc.Review(ctx, tx, row.ID, contracts.Review{Verdict: contracts.VerdictApproved, ExpectedRevision: 1})
		if err != nil {
			t.Fatalf("review: %v", err)
		}
		subject.moved = true
		_, err = svc.Apply(ctx, tx, row.ID, approved.Revision)
		if !errors.Is(err, contracts.ErrStaleBase) {
			t.Fatalf("applying a proposal whose subject moved: %v", err)
		}
		if subject.saves != 0 {
			t.Error("a stale apply wrote the subject")
		}
		still, err := svc.Get(ctx, tx, row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if still.State != contracts.StateApproved {
			t.Errorf("a refused apply left the proposal %s, and it is still waiting", still.State)
		}
		if got := published(t, tx); len(got) != 2 {
			t.Errorf("a refused apply published %v, and a refusal says nothing", got)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

// TestDuplicateOpenProposalIsTheSameProposal is the screen that submits twice.
func TestDuplicateOpenProposalIsTheSameProposal(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		first := propose(t, subject, svc, ctx, tx)
		second := propose(t, subject, svc, ctx, tx)
		if first.ID != second.ID {
			t.Fatalf("the same change produced two proposals: %s and %s", first.ID, second.ID)
		}
		if got := published(t, tx); len(got) != 1 {
			t.Errorf("the duplicate published %v", got)
		}
		// A different diff over the same subject is a second opinion, and both stand.
		if _, err := svc.Propose(ctx, tx, contracts.NewProposal{
			SubjectModule: "counter", SubjectEntity: "one",
			Diff: diff(t, `{"limit":12}`), Summary: "raise it by two",
		}); err != nil {
			t.Fatalf("a second opinion: %v", err)
		}
		rows, total, err := svc.List(ctx, tx, contracts.Query{SubjectModule: "counter"})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 2 || len(rows) != 2 {
			t.Fatalf("two opinions over one subject read as %d rows", total)
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

// TestUnboundSubjectIsRefused keeps the composition honest: a proposal cannot be
// recorded for a subject this composition would not apply.
func TestUnboundSubjectIsRefused(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	svc := internal.NewService(nil)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		_, err := svc.Propose(ctx, tx, contracts.NewProposal{
			SubjectModule: "counter", SubjectEntity: "one",
			Diff: diff(t, `{"limit":11}`), Summary: "nothing will apply this",
		})
		if !errors.Is(err, contracts.ErrUnsupportedSubject) {
			return errors.New("an unbound subject was accepted: " + err.Error())
		}
		if got := published(t, tx); len(got) != 0 {
			return errors.New("a refused proposal published " + got[0])
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}

// TestAnotherTenantCannotReachIt is the tenant-first line of the pillar contract:
// the id is known, and the answer is still not-found and an empty page.
func TestAnotherTenantCannotReachIt(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	subject := newCounter()
	svc := internal.NewService(bindingTo(subject))
	var id uuid.UUID
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, proposer)
		id = propose(t, subject, svc, ctx, tx).ID
		return nil
	}); err != nil {
		t.Fatalf("proposing as acme: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithActor(ctx, reviewer)
		if _, err := svc.Get(ctx, tx, id); !errors.Is(err, crud.ErrNotFound) {
			return errors.New("another tenant read the row: " + err.Error())
		}
		if _, total, err := svc.List(ctx, tx, contracts.Query{}); err != nil || total != 0 {
			return errors.New("another tenant listed the row")
		}
		if _, err := svc.Withdraw(ctx, tx, id, 1); !errors.Is(err, crud.ErrNotFound) {
			return errors.New("another tenant withdrew it: " + err.Error())
		}
		return errRollback
	}); !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}

// published is the outbox of this transaction, in the order it was written: an
// event is exactly as visible as the change it describes, which is the property the
// idempotent paths are tested against.
func published(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	if err := tx.DB().Raw(`SELECT name FROM platformkit_outbox WHERE tenant_id = ? ORDER BY created_at, id`,
		db.TenantOf(tx).ID).Scan(&names).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// errRollback ends a case's transaction without committing it, so the next case
// starts from an empty schema rather than from this one's rows.
var errRollback = errors.New("rolled back on purpose")
