package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// TestRetentionMarksAreTheirTenantsOnly: a mark records one tenant's expiry, and a
// second tenant's transaction neither reads it nor writes one in the first's name —
// not as the application role, and not as the expiry role.
func TestRetentionMarksAreTheirTenantsOnly(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	seed(t, tenancy.WithTenant(t.Context(), acme), conn, internal.NewService(), uuid.New(), db.Now().AddDate(-2, 0, 0))
	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	if err := internal.Retention(lister{acme}, 365, retainURL).Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job: %v", err)
	}
	retain, err := db.Open(t.Context(), retainURL)
	if err != nil {
		t.Fatal(err)
	}
	defer retain.Close()

	globex := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "globex"})
	for label, c := range map[string]*db.Conn{"application": conn, "expiry": retain} {
		var seen int64
		if err := db.Run(globex, c, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Raw("SELECT count(*) FROM audit_retention_marks").Scan(&seen).Error
		}); err != nil {
			t.Fatalf("%s: read the marks as globex: %v", label, err)
		}
		if seen != 0 {
			t.Errorf("the %s role in globex's transaction reads %d of acme's marks", label, seen)
		}
	}
	if err := db.Run(globex, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO audit_retention_marks (tenant_id, cutoff, removed) VALUES (?, now(), 7)", acme.ID).Error
	}); err == nil {
		t.Error("globex's transaction wrote a mark in acme's name")
	}
	var mine int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_retention_marks WHERE tenant_id = $1`, acme.ID).Scan(&mine); err != nil {
		t.Fatal(err)
	}
	if mine != 1 {
		t.Errorf("acme holds %d marks, want the one its own expiry wrote", mine)
	}
}
