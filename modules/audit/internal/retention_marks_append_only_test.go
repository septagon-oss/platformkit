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

// TestRetentionMarksRefuseARewrite holds migrations/00042's claim that the record of
// an expiry "is append-only in the same sense audit_events is, and it is fenced the
// same way": the table's owner may not rewrite or remove a mark, and the application
// role may not either once an operator hands the privileges back.
func TestRetentionMarksRefuseARewrite(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	seed(t, ctx, conn, internal.NewService(), uuid.New(), db.Now().AddDate(-2, 0, 0))
	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	if err := internal.Retention(lister{acme}, 365, retainURL).Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job: %v", err)
	}

	if _, err := admin.ExecContext(t.Context(), `UPDATE audit_retention_marks SET removed = 0`); err == nil {
		t.Error("the table's owner rewrote a retention mark")
	}
	if _, err := admin.ExecContext(t.Context(), `DELETE FROM audit_retention_marks`); err == nil {
		t.Error("the table's owner deleted a retention mark")
	}
	role := dbtest.RoleOf(t, appURL(t))
	if _, err := admin.ExecContext(t.Context(), "GRANT ALL ON TABLE audit_retention_marks TO "+role); err != nil {
		t.Fatalf("grant the marks back to the application role: %v", err)
	}
	for _, stmt := range []string{
		"UPDATE audit_retention_marks SET removed = 0",
		"DELETE FROM audit_retention_marks",
		"TRUNCATE audit_retention_marks",
	} {
		if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(stmt).Error
		}); err == nil {
			t.Errorf("%q committed from a tenant transaction after GRANT ALL", stmt)
		}
	}

	var marks int
	var removed int64
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*), coalesce(sum(removed), 0) FROM audit_retention_marks`).Scan(&marks, &removed); err != nil {
		t.Fatalf("read the marks: %v", err)
	}
	if marks != 1 || removed != 1 {
		t.Errorf("the marks hold %d rows naming %d removals, want the one mark naming the one expired row", marks, removed)
	}
}
