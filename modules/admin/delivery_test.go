package admin_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
)

type deliveryAuthority struct {
	allowed bool
	called  bool
}

func (a *deliveryAuthority) Allowed(_ context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	if grant.Permission == admin.PermissionDeliveryRead {
		a.called = true
		if !grant.Operator {
			panic("delivery permission lost its operator scope")
		}
		return a.allowed, nil
	}
	return false, nil
}

func deliveryState(t *testing.T, owner *sql.DB) string {
	t.Helper()
	var state string
	if err := owner.QueryRowContext(t.Context(), `SELECT jsonb_build_array(
		(SELECT jsonb_agg(r ORDER BY id) FROM platformkit_outbox r),
		(SELECT jsonb_agg(r ORDER BY event_id, durable) FROM platformkit_handled r),
		(SELECT jsonb_agg(r ORDER BY event_id, durable) FROM platformkit_dead_letters r))::text`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDeliveryInspectionRequiresOperatorGrantForRouteAndNavigation(t *testing.T) {
	manifest := admin.Module(admin.Deps{})
	found := false
	for _, grant := range manifest.Permissions {
		if grant.Key == admin.PermissionDeliveryRead {
			found = grant.Operator
		}
	}
	if !found || len(manifest.Nav) != 1 || manifest.Nav[0].Permission != admin.PermissionDeliveryRead {
		t.Fatal("delivery manifest lost its single operator navigation entry")
	}
	for _, c := range []struct {
		name, host string
		allowed    bool
		status     int
	}{{"operator", operatorHost, true, http.StatusOK}, {"ungranted operator", operatorHost, false, http.StatusForbidden},
		{"tenant wildcard", host, true, http.StatusForbidden}} {
		t.Run(c.name, func(t *testing.T) {
			authority := &deliveryAuthority{allowed: c.allowed}
			router := mountAs(t, authority)
			status, body, _ := callAt(t, router, c.host, http.MethodGet, "/admin", "")
			if status != http.StatusOK || strings.Contains(body, `href="/admin/delivery"`) != (c.status == http.StatusOK) {
				t.Fatalf("navigation = %d %s", status, body)
			}
			status, body, _ = callAt(t, router, c.host, http.MethodGet, "/admin/delivery", "")
			if status != c.status {
				t.Fatalf("direct route = %d %s", status, body)
			}
			if c.host == host && authority.called {
				t.Fatal("customer tenant reached the operator authorizer")
			}
			if c.status != http.StatusOK && strings.Contains(body, "No terminal failure records.") {
				t.Fatal("refusal exposed the report")
			}
			if c.status == http.StatusOK && (!strings.Contains(body, "No pending publication records.") || !strings.Contains(body, "No terminal failure records.")) {
				t.Fatal("empty inspection was not rendered")
			}
		})
	}
}

func TestDeliveryInspectionShowsBoundedEscapedMetadataAndPreservesRecords(t *testing.T) {
	router, owner := mountWithDatabase(t, &deliveryAuthority{allowed: true})
	for range 51 {
		id := uuid.New()
		if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_outbox(id, tenant_id, name, payload)
			VALUES ($1, $2, 'fixture.pending', '{"secret":"private-payload"}')`, id, acme.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(t.Context(), `INSERT INTO platformkit_dead_letters(event_id, durable, tenant_id, name, error)
			VALUES ($1, '<private-durable>', $2, 'fixture.failed', 'password=private-cause')`, id, acme.ID); err != nil {
			t.Fatal(err)
		}
	}
	before := deliveryState(t, owner)
	req := httptest.NewRequest(http.MethodGet, "http://"+operatorHost+"/admin/delivery", nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("inspection status/privacy = %d %v", w.Code, w.Header())
	}
	for _, wanted := range []string{"Observed at ", "Pending publication", "Terminal failures", "Refresh records", "&lt;private-durable&gt;", acme.ID.String()} {
		if !strings.Contains(body, wanted) {
			t.Errorf("inspection omitted %q", wanted)
		}
	}
	for _, excluded := range []string{"private-payload", "private-cause", "password=", "<private-durable>"} {
		if strings.Contains(body, excluded) {
			t.Errorf("inspection exposed %q", excluded)
		}
	}
	if strings.Count(body, "Showing 50 records. More records exist.") != 2 || strings.Count(body, "data-pk-row=") != 100 {
		t.Fatal("inspection lost its per-list bound or truncation notice")
	}
	if after := deliveryState(t, owner); after != before {
		t.Fatal("inspection changed delivery records")
	}
	if _, err := owner.ExecContext(t.Context(), "ALTER TABLE platformkit_dead_letters RENAME TO inspection_unavailable"); err != nil {
		t.Fatal(err)
	}
	status, body, _ := callAt(t, router, operatorHost, http.MethodGet, "/admin/delivery", "")
	if status != http.StatusServiceUnavailable || strings.Contains(body, "fixture.pending") || strings.Contains(body, "inspection_unavailable") {
		t.Fatalf("unavailable inspection leaked partial data or query details: %d %s", status, body)
	}
}
