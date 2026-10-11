package main

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTheTrailExpiresWithTheGrantsTheAuditREADMENames runs the reference
// composition's own audit-retention job, with its own tenant lister, as an expiry
// role provisioned exactly as modules/audit/README.md's Provisioning section says,
// and finds the aged row gone and its mark written.
func TestTheTrailExpiresWithTheGrantsTheAuditREADMENames(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	admin := dbtest.OpenFor(t, cfg.Database.MigrateURL)

	var tenant uuid.UUID
	if err := admin.QueryRowContext(t.Context(), `SELECT id FROM tenants WHERE slug = 'acme'`).Scan(&tenant); err != nil {
		t.Fatalf("find the installed tenant: %v", err)
	}
	aged := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO audit_events (tenant_id, occurred_at, name, event_id, payload) VALUES ($1, now() - interval '400 days', 'task.task.created', $2, '{}')`,
		tenant, aged); err != nil {
		t.Fatalf("seed an aged trail row: %v", err)
	}

	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	cfg.Database.RetainURL = retainURL
	var ran bool
	for _, m := range compose(cfg).modules {
		for _, j := range m.Jobs {
			if j.Name != "audit-retention" {
				continue
			}
			ran = true
			conn, err := db.Open(t.Context(), cfg.Database.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := j.Run(t.Context(), conn); err != nil {
				t.Fatalf("the reference composition's retention job, provisioned as documented: %v", err)
			}
		}
	}
	if !ran {
		t.Fatal("the reference composition carries no audit-retention job")
	}
	var left, marks int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE event_id = $1`, aged).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_retention_marks WHERE tenant_id = $1`, tenant).Scan(&marks); err != nil {
		t.Fatal(err)
	}
	if left != 0 || marks != 1 {
		t.Errorf("after the pass the aged row is present %d time(s) and the tenant holds %d mark(s), want 0 and 1", left, marks)
	}
}
