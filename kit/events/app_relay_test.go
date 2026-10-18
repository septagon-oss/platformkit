// The relay is the one place in the events path that walks rows it did not write,
// so it is where two compositions sharing a database meet. Claiming "every
// unpublished row" there publishes another app's event at that app's own address
// and stamps the row: the message is refused at delivery, because the address
// carries the app, but the row is gone from this process's view and the app that
// owns it never learns there was work. The claim is the boundary.
//
// The tenant is joined and not required: a row whose tenant row is gone names no
// app, so it belongs to the deployment of one app — which is every such row until
// this argument existed — and to no app that names itself.
package events_test

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// stillPending counts the whole queue, not one tenant's slice of it: `pending`
// reads through a tenant's own context, which is the right scope for a tenant's
// queue and the wrong one for a claim that spans tenants.
func stillPending(t *testing.T, admin *sql.DB) int {
	t.Helper()
	var n int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count pending rows: %v", err)
	}
	return n
}

func TestARelayClaimsOnlyItsOwnAppTenantsRows(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	tenant := func(slug, host, app string) tenancy.Tenant {
		id := uuid.New()
		if _, err := admin.ExecContext(t.Context(),
			`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, $3)`, id, slug, app); err != nil {
			t.Fatalf("a tenant of app %s: %v", app, err)
		}
		if _, err := admin.ExecContext(t.Context(),
			`INSERT INTO tenant_hosts (host, tenant_id, is_primary) VALUES ($1, $2, true)`, host, id); err != nil {
			t.Fatalf("a host for %s: %v", slug, err)
		}
		return tenancy.Tenant{ID: id, Slug: slug, Name: slug}
	}
	shelf := tenant("relay-shelf", "shelf.example.com", "shelf")
	collect := tenant("relay-collect", "collect.example.com", "collect")

	publish(t, conn, shelf, "billing.plan.created", map[string]any{"n": 1})
	publish(t, conn, collect, "billing.plan.created", map[string]any{"n": 2})
	// A row whose tenant is not in the control plane at all: nobody's app, and the
	// deployment of one app keeps the obligation to move it.
	orphans := uuid.New()
	publish(t, conn, tenancy.Tenant{ID: orphans, Slug: "ghost", Name: "Ghost"},
		"billing.plan.created", map[string]any{"n": 3})

	// Each app relays its own rows and no others, twice over: a pass must not
	// re-publish what it stamped, and must not reach into the other's pending rows
	// while it is at it.
	for _, one := range []string{"shelf", "collect"} {
		rec := &recorder{}
		if err := events.RelayApp(t.Context(), conn, rec, appname.MustParse(one)); err != nil {
			t.Fatalf("relay as %s: %v", one, err)
		}
		if got := len(rec.names()); got != 1 {
			t.Errorf("app %s claimed %d rows, want the one its tenant wrote", one, got)
		}
	}
	if left := stillPending(t, admin); left != 1 {
		t.Errorf("%d rows are still pending after both apps relayed, want only the orphan that names no app", left)
	}

	// An app-scoped pass never took the orphan, and the deployment of one app —
	// which is every composition that sets no slug — does: it is the only party that
	// can, because the row names no app to disagree with.
	empty := &recorder{}
	if err := events.RelayApp(t.Context(), conn, empty, "acme"); err != nil {
		t.Fatalf("relay as acme: %v", err)
	}
	if got := len(empty.names()); got != 0 {
		t.Errorf("app acme claimed %d rows, want none: two tenants of other apps and one orphan are not its work", got)
	}
	unlabelled := &recorder{}
	if err := events.Relay(t.Context(), conn, unlabelled); err != nil {
		t.Fatalf("relay as the deployment of one app: %v", err)
	}
	if got := len(unlabelled.names()); got != 1 {
		t.Errorf("the unlabelled deployment claimed %d rows, want the one row whose tenant names no app", got)
	}
	if left := stillPending(t, admin); left != 0 {
		t.Errorf("%d rows are pending after every app and the unlabelled deployment relayed", left)
	}
}
