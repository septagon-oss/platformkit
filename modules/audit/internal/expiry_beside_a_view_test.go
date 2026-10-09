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

// The statement trigger that refuses an expiry by a role appending through a view over
// the trail must not refuse the expiry role itself when such a view exists and another
// role holds INSERT on it. TestExpiryRoleIsTheOnlyDoor builds no view, so without this
// case a walk that over-refused would leave the trail with no expiry at all in every
// deployment that reads it through a view.
func TestExpiryRoleStillExpiresBesideAViewOverTheTrail(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	aged := uuid.New()
	seed(t, ctx, conn, internal.NewService(), aged, db.Now().AddDate(-2, 0, 0))
	writer := "w_" + dbtest.DeploymentSchema(t, admin)
	t.Cleanup(func() {
		ctx := context.WithoutCancel(t.Context())
		_, _ = admin.ExecContext(ctx, `DROP OWNED BY "`+writer+`"`)
		_, _ = admin.ExecContext(ctx, `DROP ROLE IF EXISTS "`+writer+`"`)
	})
	for _, q := range []string{
		"CREATE VIEW audit_events_reader AS SELECT * FROM audit_events",
		"CREATE VIEW audit_events_reader_deep AS SELECT * FROM audit_events_reader",
		`CREATE ROLE "` + writer + `" NOLOGIN`,
		`GRANT SELECT, INSERT ON audit_events_reader_deep TO "` + writer + `"`,
	} {
		if _, err := admin.ExecContext(t.Context(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks",
		"SELECT ON audit_events_reader, audit_events_reader_deep")
	retain, err := db.Open(t.Context(), retainURL)
	if err != nil {
		t.Fatal(err)
	}
	defer retain.Close()
	var removed int64
	err = db.Run(ctx, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", aged)
		removed = res.RowsAffected
		return res.Error
	})
	if err != nil || removed != 1 {
		t.Fatalf("the expiry role, holding no append door, expires an aged row beside a view: %v (rows %d)", err, removed)
	}
}
