package internal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// TestExportingAPersonFindsTheProposalsTheyMade seeds one proposal a person made and
// asks change's personal-data part for that person: the part names the proposal, and
// the erasure that follows removes their address from its summary.
func TestExportingAPersonFindsTheProposalsTheyMade(t *testing.T) {
	_, conn := dbtest.Schema(t, change.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones"}
	proposal := uuid.New()

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO change_proposals
			(id, tenant_id, subject_module, subject_entity, subject_id, base_revision, diff, diff_digest, summary, proposer)
			VALUES (?, ?, 'task', 'task', ?, 1, '{}', 'digest', ?, ?)`,
			proposal, acme.ID, uuid.New(), "retitle, asked by jo.jones@example.com", subject.ID).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var part json.RawMessage
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		part, err = internal.PersonalData().Data.Export(ctx, tx, subject)
		return err
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(string(part), proposal.String()) {
		t.Errorf("the part does not name the proposal the person made: %s", part)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}
	var left int
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM change_proposals WHERE summary ILIKE '%jo.jones@example.com%'`).Scan(&left).Error
	}); err != nil || left != 0 {
		t.Errorf("%d proposals still carry the address after the erasure (%v)", left, err)
	}
}
