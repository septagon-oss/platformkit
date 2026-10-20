package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestErasureKeepsTheAuditRowAnonymised is the brief's acceptance at the trail's own
// door: the person's audit row survives the erasure with its actor id, and neither the
// address in its payload nor the client address it was recorded from is left in it.
func TestErasureKeepsTheAuditRowAnonymised(t *testing.T) {
	const email = "Jo.Jones@Example.com"
	_, conn := dbtest.Schema(t, audit.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones", Handle: "jjones"}
	row := uuid.New()

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO audit_events (id, tenant_id, occurred_at, name, actor, event_id, payload, client_ip)
			VALUES (?, ?, now(), 'user.updated', ?, ?, ?::jsonb, '203.0.113.7')`,
			row, acme.ID, subject.ID, uuid.New(), `{"email":"`+email+`","displayName":"Jo Jones","active":true}`).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var kept, named, located int
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT count(*) FROM audit_events WHERE id = ? AND actor = ? AND jsonb_exists(payload, 'email')`,
			row, subject.ID).Scan(&kept).Error; err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT count(*) FROM audit_events
			WHERE strpos(lower(payload::text), 'jo.jones@example.com') > 0 OR strpos(payload::text, 'Jo Jones') > 0`).
			Scan(&named).Error; err != nil {
			return err
		}
		return tx.DB().Raw(`SELECT count(*) FROM audit_events WHERE client_ip IS NOT NULL`).Scan(&located).Error
	}); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if kept != 1 {
		t.Errorf("the audit row did not survive with its actor and its document's shape")
	}
	if named != 0 || located != 0 {
		t.Errorf("after the erasure %d rows still name the person and %d still carry a client address", named, located)
	}
}
