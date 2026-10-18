package events_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTwoAppRelaysKeepConcurrentClaimsInsideTheirOwnApps covers the shared
// database boundary while both apps claim the same event name at the same time.
func TestTwoAppRelaysKeepConcurrentClaimsInsideTheirOwnApps(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	const name = "billing.plan.created"
	const perApp = 12

	tenantOf := func(slug string) tenancy.Tenant {
		t.Helper()
		id := uuid.New()
		if _, err := admin.ExecContext(t.Context(),
			`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, $2)`, id, slug); err != nil {
			t.Fatalf("place tenant in app %s: %v", slug, err)
		}
		return tenancy.Tenant{ID: id, Slug: slug, Name: slug}
	}
	one := tenantOf("acme")
	two := tenantOf("academy")
	for range perApp {
		publish(t, conn, one, name, nil)
		publish(t, conn, two, name, nil)
	}

	first, second := &recorder{}, &recorder{}
	start := make(chan struct{})
	done := make(chan error, 2)
	for _, job := range []struct {
		app appname.Name
		out *recorder
	}{
		{appname.MustParse("acme"), first},
		{appname.MustParse("academy"), second},
	} {
		go func() {
			<-start
			done <- events.RelayApp(t.Context(), conn, job.out, job.app)
		}()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("relay: %v", err)
		}
	}

	check := func(app string, out *recorder, tenant uuid.UUID) {
		t.Helper()
		out.mu.Lock()
		defer out.mu.Unlock()
		if len(out.got) != perApp {
			t.Errorf("app %s claimed %d events, want %d of its own", app, len(out.got), perApp)
		}
		for _, ev := range out.got {
			if ev.TenantID != tenant {
				t.Errorf("app %s claimed event %s of tenant %s, want only tenant %s", app, ev.ID, ev.TenantID, tenant)
			}
		}
	}
	check("acme", first, one.ID)
	check("academy", second, two.ID)

	var pending, published int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FILTER (WHERE published_at IS NULL),
		        count(*) FILTER (WHERE published_at IS NOT NULL)
		 FROM platformkit_outbox`).Scan(&pending, &published); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if pending != 0 || published != 2*perApp {
		t.Errorf("after both relays: %d pending and %d published rows, want 0 and %d", pending, published, 2*perApp)
	}
}
