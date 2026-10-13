package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
)

func TestInstallationDeliveryCommandsReturnTheirJSONReceipts(t *testing.T) {
	router, owner := installationDelivery(t)
	tenantID, eventID := uuid.New(), uuid.New()
	if _, err := owner.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', 'collect')`, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(t.Context(),
		`INSERT INTO platformkit_outbox (id, tenant_id, name, payload, published_at) VALUES ($1, $2, 'ledger.invoice_issued', '{}', now())`, eventID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(t.Context(),
		`INSERT INTO platformkit_handled (event_id, tenant_id, durable) VALUES ($1, $2, $3)`, eventID, tenantID,
		appname.Durable("", "ledger", "ledger.invoice_issued")); err != nil {
		t.Fatal(err)
	}
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
	for _, command := range []struct {
		name   string
		suffix string
		want   map[string]any
	}{
		{"move", "/ledger/move", map[string]any{"subscriptions": float64(1), "claims": float64(1), "dead": float64(0), "tenants": float64(1)}},
		{"already_moved", "/ledger/move", map[string]any{"subscriptions": float64(0), "claims": float64(0), "dead": float64(0), "tenants": float64(0)}},
		{"replay", "/events/{id}/replay", map[string]any{"eventId": eventID.String(), "name": "ledger.invoice_issued", "reason": "delivery repaired"}},
	} {
		t.Run(command.name, func(t *testing.T) {
			var route string
			for path, methods := range doc.Paths {
				if strings.HasSuffix(path, command.suffix) && methods["post"] != nil {
					route = strings.ReplaceAll(path, "{id}", eventID.String())
				}
			}
			if route == "" {
				t.Fatal("the delivery command has no mounted route")
			}
			req := httptest.NewRequest(http.MethodPost, "http://"+tenantHost+route,
				strings.NewReader(`{"app":"collect","reason":"delivery repaired"}`))
			req.Header.Set("Authorization", "Bearer operator")
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != http.StatusOK {
				t.Fatalf("command refused: %d %s", res.Code, res.Body.String())
			}
			var receipt map[string]any
			if err := json.Unmarshal(res.Body.Bytes(), &receipt); err != nil {
				t.Fatalf("successful command returned no JSON receipt: status=%d body=%q error=%v", res.Code, res.Body.String(), err)
			}
			for field, want := range command.want {
				if got := receipt[field]; got != want {
					t.Errorf("receipt %s=%v, want %v", field, got, want)
				}
			}
		})
	}
}
