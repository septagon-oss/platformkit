// The relay's claim reads two things written for different reasons: the tenant's
// app, which keeps one composition out of another's rows, and the request columns,
// which let a delivery name the call that caused it. Both live in one query, and a
// query that keeps one of them by dropping the other still compiles.
package events_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestARelayCarriesTheRequestOnlyOfItsOwnAppsRows(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := func(slug, app string) tenancy.Tenant {
		id := uuid.New()
		if _, err := admin.ExecContext(t.Context(),
			`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, $3)`, id, slug, app); err != nil {
			t.Fatalf("a tenant of app %s: %v", app, err)
		}
		return tenancy.Tenant{ID: id, Slug: slug, Name: slug}
	}
	shelf := tenant("request-shelf", "shelf")
	collect := tenant("request-collect", "collect")

	// Each row is written inside a call of its own, so a row that arrives with the
	// other's request id was read through the wrong row.
	write := func(owner tenancy.Tenant, id, addr string) {
		t.Helper()
		ctx := request.With(tenancy.WithTenant(t.Context(), owner), request.Context{ID: id, ClientAddr: addr})
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "billing.plan.created", map[string]any{"by": owner.Slug})
		}); err != nil {
			t.Fatalf("publish as %s: %v", owner.Slug, err)
		}
	}
	write(shelf, "req-shelf", "192.0.2.10")
	write(collect, "req-collect", "198.51.100.20")

	for _, c := range []struct {
		app, id, addr string
		tenant        uuid.UUID
	}{
		{"shelf", "req-shelf", "192.0.2.10", shelf.ID},
		{"collect", "req-collect", "198.51.100.20", collect.ID},
	} {
		rec := &recorder{}
		if err := events.RelayApp(t.Context(), conn, rec, appname.MustParse(c.app)); err != nil {
			t.Fatalf("relay as %s: %v", c.app, err)
		}
		rec.mu.Lock()
		got := append([]events.Event(nil), rec.got...)
		rec.mu.Unlock()
		if len(got) != 1 {
			t.Fatalf("app %s published %d events, want the one its tenant wrote", c.app, len(got))
		}
		ev := got[0]
		if ev.TenantID != c.tenant {
			t.Errorf("app %s published tenant %s's event, want its own tenant %s", c.app, ev.TenantID, c.tenant)
		}
		if ev.RequestID != c.id || ev.ClientIP != c.addr {
			t.Errorf("app %s's event names request %q from %q, want %q from %q",
				c.app, ev.RequestID, ev.ClientIP, c.id, c.addr)
		}
	}
	if left := stillPending(t, admin); left != 0 {
		t.Errorf("%d rows are still pending after each app relayed its own", left)
	}
}
