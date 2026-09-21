package rest_test

// The delete door's turn at the question the fifth review asked of the patch
// door. Row-level security filters a DELETE by a policy's USING clause alone —
// a WITH CHECK clause inspects the row a write produces, and a delete produces
// none — so on the catalogue shape, whose USING is deliberately true for every
// tenant, GetForUpdate answers a row the request may read and may not remove,
// and there was no second check between the two. Measured before the recheck, on
// this pair of mounts: the hard delete answered 204 and the row left the table,
// event and all; the soft one answered 500 out of a policy violation and kept the
// row. Both are answers the update door had already stopped giving, and 404 is
// what the same door says about a row nobody else owns.

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// mountDeleteCatalog is the same catalogue on the same policy with the
// soft-delete flag the caller asks for, because a delete reaches the database as
// one of two statements and the policy only checks one of them.
func mountDeleteCatalog(t *testing.T, soft bool) (http.Handler, *sql.DB) {
	t.Helper()
	admin, conn := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), planDDL); err != nil {
		t.Fatalf("create the catalogue: %v", err)
	}
	reader := tenancy.Tenant{ID: uuid.New(), Slug: "reader", Name: "Reader"}
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Tenants:    hosts{map[string]tenancy.Tenant{host: acme, "reader.test": reader}},
		Conn:       conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	rest.Spec[*Plan]{
		Module: "billing", Entity: "plan", Path: "/api/v1/billing/plans",
		Read: "billing:read", Write: "billing:catalog", SoftDelete: soft,
	}.Mount(api)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, admin
}

// deleteDoorRefused. The row is created by the tenant that owns it, deleted at by
// the tenant that only reads it, and read back by both.
func deleteDoorRefused(t *testing.T, soft bool) {
	t.Helper()
	router, admin := mountDeleteCatalog(t, soft)
	code, body := call(t, router, http.MethodPost, "/api/v1/billing/plans", `{"name":"Team","cents":4000}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/billing/plans/" + id(t, body)
	key := at[strings.LastIndex(at, "/")+1:]

	if code, out := askAs(t, router, "reader.test", http.MethodDelete, at, ""); code != http.StatusNotFound ||
		strings.Contains(out, "Team") {
		t.Fatalf("reader tenant DELETE = %d %s, want 404 naming no part of the row", code, out)
	}
	var live int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_review5_plans WHERE id = $1 AND deleted_at IS NULL`, key).Scan(&live); err != nil {
		t.Fatalf("read back the row the refused delete was about: %v", err)
	}
	if live != 1 {
		t.Errorf("the refused delete left %d live copies of the row, want the one row still there", live)
	}
	if n := count(t, admin, "billing.plan.deleted"); n != 0 {
		t.Errorf("the refused delete published %d billing.plan.deleted events, want none", n)
	}

	// The control: the tenant the row names does delete it, so the 404 above is
	// ownership and not a route that stopped working.
	if code, out := call(t, router, http.MethodDelete, at, ""); code != http.StatusNoContent {
		t.Fatalf("owner DELETE = %d %s, want 204", code, out)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM rest_review5_plans WHERE id = $1 AND deleted_at IS NULL`, key).Scan(&live); err != nil {
		t.Fatalf("read back the deleted row: %v", err)
	}
	if live != 0 {
		t.Errorf("the owner's delete left the row live: the door above refused a row this one removed")
	}
	if n := count(t, admin, "billing.plan.deleted"); n != 1 {
		t.Errorf("the owner's delete published %d billing.plan.deleted events, want the one it owes", n)
	}
}

func TestTheDeleteDoorRefusesARowAnotherTenantOnlyReads(t *testing.T) {
	deleteDoorRefused(t, false)
}

func TestTheSoftDeleteDoorRefusesARowAnotherTenantOnlyReads(t *testing.T) {
	deleteDoorRefused(t, true)
}
