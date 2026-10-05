package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
)

func TestTheInstallationOffersTheLedgerMoveAndEventReplay(t *testing.T) {
	cfg, opts := compose(t)
	cfg.NATS.App = "collect"
	opts.Role = Web
	mods := []module.Module{brand("ledger", "sheet")}
	if err := Migrate(t.Context(), cfg, mods); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	a, err := New(t.Context(), cfg, mods, opts)
	if err != nil {
		t.Fatal(err)
	}
	router, err := a.buildAPI(t.Context(), conn, cache.Memory("pkit"))
	if err != nil {
		t.Fatal(err)
	}
	code, body := ask(router, tenantHost, "/openapi.json")
	if code != http.StatusOK {
		t.Fatalf("read the composed route declarations: %d %s", code, body)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	// Discover each verb from the typed router's document rather than requiring
	// a second hard-coded spelling of its surface prefix.
	for _, verb := range []struct {
		name    string
		matches func(string) bool
	}{
		{"ledger move", func(path string) bool { return strings.HasSuffix(path, "/ledger/move") }},
		{"event replay", func(path string) bool {
			return strings.Contains(path, "/events/{") && strings.HasSuffix(path, "}/replay")
		}},
	} {
		found := false
		for path, methods := range doc.Paths {
			if !verb.matches(path) || methods["post"] == nil {
				continue
			}
			found = true
			req := httptest.NewRequest(http.MethodPost, "http://"+tenantHost+path, strings.NewReader(`{"app":"collect","reason":"retry"}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s is reachable at a tenant host: %d %s", verb.name, rec.Code, rec.Body.String())
			}
		}
		if !found {
			t.Errorf("the composed installation declares no POST route for %s", verb.name)
		}
	}
}
