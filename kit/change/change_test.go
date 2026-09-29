package change_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/change"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	ada, bob    = uuid.New(), uuid.New()
	errRollback = errors.New("rolled back on purpose")
)

func as(ctx context.Context, who uuid.UUID) context.Context {
	return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: who})
}

func published(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error; err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// inTenant runs body in one tenant transaction that is rolled back afterwards.
func inTenant(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, body func(ctx context.Context, tx db.Tx[db.Tenant])) {
	t.Helper()
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		body(ctx, tx)
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("the case's transaction: %v", err)
	}
}

// TestAChangeIsProposedReviewedByAnotherAndAppliedAtTheReviewedRevision is the lifecycle, end to end: Ada
// proposes a fee at revision "r1", Bob approves it, and the owner's write runs with the stored diff while
// the fee is still at r1. Each transition publishes its event, which is what puts it in the audit trail.
func TestAChangeIsProposedReviewedByAnotherAndAppliedAtTheReviewedRevision(t *testing.T) {
	_, conn := dbtest.Schema(t)
	inTenant(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		p, err := change.Propose(as(ctx, ada), tx, change.Proposal{
			SubjectKind: "fee", SubjectID: "consultation", Revision: "r1",
			Diff: map[string]int{"amountCents": 12000}, Summary: "raise the consultation fee",
		})
		if err != nil {
			t.Fatalf("propose: %v", err)
		}
		if p.State != change.StatePending || p.ProposedBy != ada {
			t.Fatalf("a new proposal is %s by %s", p.State, p.ProposedBy)
		}
		if _, err := change.Review(as(ctx, bob), tx, p.ID, true, "within the published band"); err != nil {
			t.Fatalf("review: %v", err)
		}
		var written map[string]int
		applied, err := change.Apply(as(ctx, bob), tx, p.ID, "r1", func(diff json.RawMessage) error {
			return json.Unmarshal(diff, &written)
		})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		if applied.State != change.StateApplied || applied.AppliedAt == nil || *applied.ReviewedBy != bob {
			t.Errorf("after apply: state %s, applied %v, reviewed by %v", applied.State, applied.AppliedAt, applied.ReviewedBy)
		}
		if written["amountCents"] != 12000 {
			t.Errorf("the owner's write was handed %v, want the proposed diff", written)
		}
		want := []string{change.EventProposed, change.EventReviewed, change.EventApplied}
		if got := published(t, tx); !slices.Equal(got, want) {
			t.Errorf("published %v, want %v", got, want)
		}
	})
}

// TestNobodyReviewsTheirOwnProposal: the proposer is refused whatever their roles, and the table refuses the
// same row if anything wrote it directly.
func TestNobodyReviewsTheirOwnProposal(t *testing.T) {
	_, conn := dbtest.Schema(t)
	inTenant(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		p, err := change.Propose(as(ctx, ada), tx, change.Proposal{SubjectKind: "fee", SubjectID: "x", Revision: "r1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := change.Review(as(ctx, ada), tx, p.ID, true, ""); !errors.Is(err, change.ErrSelfReview) {
			t.Errorf("a self-review = %v, want ErrSelfReview", err)
		}
		err = tx.DB().Exec(`UPDATE change_proposals SET reviewed_by = proposed_by WHERE id = ?`, p.ID).Error
		if err == nil {
			t.Error("the table accepted a proposal reviewed by its own proposer")
		}
	})
}

// TestAStaleRevisionIsNotApplied: a subject that moved after review is not changed by what was reviewed, and a
// proposal nobody approved is not applied at all. The owner's write never runs in either case.
func TestAStaleRevisionIsNotApplied(t *testing.T) {
	_, conn := dbtest.Schema(t)
	inTenant(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		p, err := change.Propose(as(ctx, ada), tx, change.Proposal{SubjectKind: "fee", SubjectID: "x", Revision: "r1"})
		if err != nil {
			t.Fatal(err)
		}
		ran := false
		write := func(json.RawMessage) error { ran = true; return nil }
		if _, err := change.Apply(as(ctx, bob), tx, p.ID, "r1", write); !errors.Is(err, change.ErrNotApproved) {
			t.Errorf("applying a pending proposal = %v, want ErrNotApproved", err)
		}
		if _, err := change.Review(as(ctx, bob), tx, p.ID, true, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := change.Apply(as(ctx, bob), tx, p.ID, "r2", write); !errors.Is(err, change.ErrStale) {
			t.Errorf("applying against a moved subject = %v, want ErrStale", err)
		}
		if ran {
			t.Error("the owner's write ran for a change that was refused")
		}
		if _, err := change.Review(as(ctx, bob), tx, p.ID, false, ""); !errors.Is(err, change.ErrNotPending) {
			t.Errorf("a second review = %v, want ErrNotPending", err)
		}
	})
}

// TestOnlyTheProposerWithdrawsAndAnonymousActsNot: withdrawal is the proposer's, and a request with no signed-in
// account can do nothing, because change control records who.
func TestOnlyTheProposerWithdrawsAndAnonymousActsNot(t *testing.T) {
	_, conn := dbtest.Schema(t)
	inTenant(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, err := change.Propose(ctx, tx, change.Proposal{SubjectKind: "fee", SubjectID: "x", Revision: "r1"}); !errors.Is(err, change.ErrNoPrincipal) {
			t.Errorf("an anonymous proposal = %v, want ErrNoPrincipal", err)
		}
		p, err := change.Propose(as(ctx, ada), tx, change.Proposal{SubjectKind: "fee", SubjectID: "x", Revision: "r1"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := change.Withdraw(as(ctx, bob), tx, p.ID, ""); !errors.Is(err, change.ErrNotFound) {
			t.Errorf("somebody else's withdrawal = %v, want ErrNotFound", err)
		}
		w, err := change.Withdraw(as(ctx, ada), tx, p.ID, "superseded")
		if err != nil || w.State != change.StateWithdrawn {
			t.Errorf("the proposer's withdrawal = %v, %v", w, err)
		}
	})
}

// TestOneTenantsProposalsAreNotAnothers: the same id means nothing in another tenant — by the table's row-level
// security, not by anything this package checks.
func TestOneTenantsProposalsAreNotAnothers(t *testing.T) {
	_, conn := dbtest.Schema(t)
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		p, err := change.Propose(as(ctx, ada), tx, change.Proposal{SubjectKind: "fee", SubjectID: "x", Revision: "r1"})
		if err == nil {
			id = p.ID
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	inTenant(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, err := change.Get(tx, id); !errors.Is(err, change.ErrNotFound) {
			t.Errorf("globex read acme's proposal: %v", err)
		}
		if _, err := change.Review(as(ctx, bob), tx, id, true, ""); !errors.Is(err, change.ErrNotFound) {
			t.Errorf("globex reviewed acme's proposal: %v", err)
		}
	})
}
