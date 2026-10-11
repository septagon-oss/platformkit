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

// TestTrailRefusesTruncateWhenThePrivilegeComesBack is the case migrations/000048's
// header names as the triggers' reason to exist: an operator runs GRANT ALL on the
// trail to the application role. UPDATE and DELETE are still refused by the row
// triggers; TRUNCATE must be refused too, and TRUNCATE ignores row-level security, so
// a TRUNCATE that commits from one tenant's transaction empties every tenant's trail.
func TestTrailRefusesTruncateWhenThePrivilegeComesBack(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	for _, tc := range []tenancy.Tenant{acme, globex} {
		seed(t, tenancy.WithTenant(t.Context(), tc), conn, svc, uuid.New(), db.Now())
	}
	role := dbtest.RoleOf(t, appURL(t))
	if _, err := admin.ExecContext(t.Context(), "GRANT ALL ON TABLE audit_events TO "+role); err != nil {
		t.Fatalf("grant the trail back to the application role: %v", err)
	}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("TRUNCATE audit_events").Error
	})
	if err == nil {
		t.Error("TRUNCATE of audit history committed from a tenant transaction after GRANT ALL")
	}
	var kept int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&kept); err != nil {
		t.Fatalf("count the trail: %v", err)
	}
	if kept != 2 {
		t.Errorf("the trail holds %d rows after the TRUNCATE, want both tenants' rows", kept)
	}
}
