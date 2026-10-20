package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// TestTheApplicationRoleCannotRewriteATrailDocument asks the fence the trail has
// carried since it became append-only: a tenant transaction, run as the application
// role, tries to replace what one row's document says. The statement has to be refused
// and the document has to read afterwards exactly as it did before. An erasure's scrub
// is the one rewrite the trail admits, and a document that says something else about
// what happened is not it.
func TestTheApplicationRoleCannotRewriteATrailDocument(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	row := uuid.New()
	const document = `{"email": "jo.jones@example.com", "active": true}`

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO audit_events (id, tenant_id, occurred_at, name, actor, event_id, payload, client_ip)
			VALUES (?, ?, now(), 'user.updated', ?, ?, ?::jsonb, '203.0.113.7')`,
			row, acme.ID, uuid.New(), uuid.New(), document).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	read := func() string {
		t.Helper()
		var text string
		if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw(`SELECT payload::text FROM audit_events WHERE id = ?`, row).Scan(&text).Error
		}); err != nil {
			t.Fatalf("read the row: %v", err)
		}
		return text
	}
	before := read()

	err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`UPDATE audit_events SET payload = ?::jsonb WHERE id = ?`,
			`{"email": "somebody.else@example.com", "active": false}`, row).Error
	})
	if err == nil {
		t.Errorf("the application role replaced what a trail row says happened, and the statement was accepted")
	}
	if after := read(); after != before {
		t.Errorf("the trail row's document was rewritten:\n before %s\n after  %s", before, after)
	}
}
