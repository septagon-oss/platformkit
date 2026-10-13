package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func installationDelivery(t *testing.T, permissions ...string) (http.Handler, *sql.DB) {
	t.Helper()
	cfg, opts := compose(t)
	cfg.NATS.App = "collect"
	opts.Role = Web
	opts.Installation.Host = tenantHost
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "installation", Operator: true}
	actor := uuid.New()
	opts.Tenants = fixture{tenant: operator}
	opts.Authenticate = func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{UserID: actor, Permissions: permissions}, true, nil
	}
	mods := []module.Module{brand("ledger", "sheet")}
	if err := Migrate(t.Context(), cfg, mods); err != nil {
		t.Fatal(err)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	a, err := New(t.Context(), cfg, mods, opts)
	if err != nil {
		t.Fatal(err)
	}
	store := cache.Memory("pkit")
	api, router, err := a.buildAPI(t.Context(), store)
	if err != nil {
		t.Fatal(err)
	}
	// Start hands the connection and the store to the API it just built; a test
	// that serves the installation's commands over a real database hands them
	// the same way.
	if err := api.Connect(conn, store); err != nil {
		t.Fatal(err)
	}
	return router, owner
}

func TestInstallationDeliveryCommandsRequireTheirDeclaredGrants(t *testing.T) {
	router, owner := installationDelivery(t, "ledger:read")
	code, body := ask(router, tenantHost, "/openapi.json")
	if code != http.StatusOK {
		t.Fatalf("read declarations: %d %s", code, body)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"/ledger/move", "/events/{id}/replay"} {
		t.Run(suffix, func(t *testing.T) {
			var route string
			for path, methods := range doc.Paths {
				if strings.HasSuffix(path, suffix) && methods["post"] != nil {
					route = strings.ReplaceAll(path, "{id}", uuid.NewString())
				}
			}
			if route == "" {
				t.Fatal("command is not mounted")
			}
			req := httptest.NewRequest(http.MethodPost, "http://"+tenantHost+route,
				strings.NewReader(`{"app":"collect","reason":"delivery repaired"}`))
			req.Header.Set("Authorization", "Bearer reader")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusForbidden {
				t.Errorf("credential lacking the command's grant: %d %s, want 403", res.Code, res.Body.String())
			}
			var emitted int
			if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE name IN ($1, $2)`,
				events.EventReplayed, events.EventLedgerMoved).Scan(&emitted); err != nil {
				t.Fatal(err)
			}
			if emitted != 0 {
				t.Errorf("refused delivery command emitted %d domain records", emitted)
			}
		})
	}
}

func TestAnInstallationReplayRefusesAnEventOwnedByAnotherApp(t *testing.T) {
	router, owner := installationDelivery(t)
	code, body := ask(router, tenantHost, "/openapi.json")
	if code != http.StatusOK {
		t.Fatalf("read the mounted routes: %d %s", code, body)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	var route string
	for path, methods := range doc.Paths {
		if strings.HasSuffix(path, "/events/{id}/replay") && methods["post"] != nil {
			route = path
		}
	}
	if route == "" {
		t.Fatal("the installation declares no event replay route")
	}
	for _, own := range []appname.Name{"collect", "academy"} {
		t.Run(own.String(), func(t *testing.T) {
			tenantID, eventID := uuid.New(), uuid.New()
			if _, err := owner.ExecContext(t.Context(),
				`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, $2)`, tenantID, own.String()); err != nil {
				t.Fatal(err)
			}
			durable := appname.Durable(own, "ledger", "ledger.invoice_issued")
			for _, insert := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, published_at) VALUES ($1, $2, 'ledger.invoice_issued', '{}', now())`, []any{eventID, tenantID}},
				{`INSERT INTO platformkit_handled (event_id, tenant_id, durable) VALUES ($1, $2, $3)`, []any{eventID, tenantID, durable}},
				{`INSERT INTO platformkit_dead_letters (event_id, tenant_id, durable, name, error) VALUES ($1, $2, $3, 'ledger.invoice_issued', 'delivery failed')`, []any{eventID, tenantID, durable}},
			} {
				if _, err := owner.ExecContext(t.Context(), insert.sql, insert.args...); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(http.MethodPost, "http://"+tenantHost+strings.ReplaceAll(route, "{id}", eventID.String()),
				strings.NewReader(`{"app":"collect","reason":"the delivery provider is repaired"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Authorization", "Bearer operator")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			var claims, dead, records int
			var pending bool
			if err := owner.QueryRowContext(t.Context(), `SELECT
				(SELECT count(*) FROM platformkit_handled WHERE event_id=$1),
				(SELECT count(*) FROM platformkit_dead_letters WHERE event_id=$1),
				(SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$2 AND name=$3),
				(SELECT published_at IS NULL FROM platformkit_outbox WHERE id=$1)`,
				eventID, tenantID, events.EventReplayed).Scan(&claims, &dead, &records, &pending); err != nil {
				t.Fatal(err)
			}
			if own == "collect" {
				if res.Code != http.StatusOK || claims != 0 || dead != 0 || records != 1 || !pending {
					t.Fatalf("own-app replay: status=%d claims=%d dead=%d records=%d pending=%v body=%s",
						res.Code, claims, dead, records, pending, res.Body.String())
				}
				return
			}
			if res.Code < 400 || res.Code >= 500 {
				t.Errorf("replay of academy's event through collect: status=%d body=%s; want a reasoned client refusal", res.Code, res.Body.String())
			}
			if res.Code >= 400 && res.Code < 500 {
				var refusal struct {
					Detail string `json:"detail"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &refusal); err != nil || refusal.Detail == "" {
					t.Errorf("foreign replay refusal gives no reason: %q", res.Body.String())
				}
			}
			if claims != 1 || dead != 1 || records != 0 || pending {
				t.Errorf("foreign event changed: claims=%d dead=%d replay records=%d pending=%v; want 1, 1, 0, false", claims, dead, records, pending)
			}
			if strings.Contains(res.Body.String(), `"eventId"`) || strings.Contains(res.Body.String(), `"name":"ledger.invoice_issued"`) {
				t.Errorf("foreign replay returned the event row: %s", res.Body.String())
			}
		})
	}
}
