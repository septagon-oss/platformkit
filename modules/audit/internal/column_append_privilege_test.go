package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

func TestColumnAppenderCannotExpireHistory(t *testing.T) {
	admin, _ := dbtest.Schema(t, audit.Migrations)
	_, appDSN := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		// A column list that leaves one of the appended columns out refuses the whole
		// INSERT, so this grant names the attribution four as well; what it still may not
		// do is the point of the case.
		"INSERT (tenant_id, occurred_at, name, actor, event_id, payload, records, request_id, client_ip, traceparent, actor_kind, source_file, source_line, initiator) ON TABLE audit_events")
	conn, err := db.Open(t.Context(), appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := tenancy.WithTenant(t.Context(), acme)
	id := uuid.New()
	// Prove this is an application writer through the real service. Column grants
	// permit its complete INSERT without a table-level INSERT grant.
	seed(t, ctx, conn, internal.NewService(), id, db.Now().AddDate(-2, 0, 0))
	var removed int64
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", id)
		removed = res.RowsAffected
		return res.Error
	})
	if err == nil {
		t.Errorf("a role that successfully appends audit history committed DELETE of %d rows", removed)
	} else if pg, ok := errors.AsType[*pgconn.PgError](err); !ok || pg.Code != "42501" {
		t.Errorf("expiry refusal = %v, want insufficient_privilege (42501)", err)
	}
	var kept, marks int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT (SELECT count(*) FROM audit_events WHERE event_id=$1), (SELECT count(*) FROM audit_retention_marks)", id).
		Scan(&kept, &marks); err != nil {
		t.Fatal(err)
	}
	if kept != 1 || marks != 0 {
		t.Errorf("application writer left %d history rows and %d retention marks; want the unchanged row and no marks", kept, marks)
	}
}
