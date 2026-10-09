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
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestExportingAPersonCarriesTheirAuditTrail exports a person who has one audit row:
// the part names the event and carries the payload the trail recorded.
func TestExportingAPersonCarriesTheirAuditTrail(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones"}

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO audit_events (tenant_id, occurred_at, name, actor, event_id, payload)
			VALUES (?, now(), 'user.updated', ?, ?, ?::jsonb)`,
			acme.ID, subject.ID, uuid.New(), `{"email":"jo.jones@example.com"}`).Error
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
	if !strings.Contains(string(part), "user.updated") || !strings.Contains(string(part), subject.Email) {
		t.Errorf("the part does not carry the person's audit row: %s", part)
	}
}
