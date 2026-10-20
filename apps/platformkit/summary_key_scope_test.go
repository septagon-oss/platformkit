package main

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// summary_key_scope_test.go asks the counting door with a key after the key's
// holder has stored the tenant's counts through a session. The door admits any
// credential the installation recognises, and its store is keyed by tenant, so a
// key narrower than the person who minted it is the caller a stored count could be
// handed past its scopes: each key must be named exactly the cards its own
// catalogue gives it, and a card it may read carries the count its holder was given.

// askWithKey is one GET at acme on a key alone, with no cookie jar behind it.
func askWithKey(t *testing.T, cfg config.Config, key, path string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Authorization", "Bearer "+key)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s on a key: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the body: %v", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s on a key = %d %s, want 200", path, res.StatusCode, body)
	}
	return body
}

// cardsForKey is the summary's entries for one key, and the list-bearing entries
// of the same key's catalogue, both sorted in the `<module>/<entity>` spelling.
func cardsForKey(t *testing.T, cfg config.Config, key string) ([]string, map[string]*int64, []string) {
	t.Helper()
	var sum screens.Summary
	if err := json.Unmarshal(askWithKey(t, cfg, key, summaryPath), &sum); err != nil {
		t.Fatalf("the summary body is not the document: %v", err)
	}
	named, totals := []string{}, map[string]*int64{}
	for _, e := range sum.Entries {
		named = append(named, e.Module+"/"+e.Entity)
		totals[e.Module+"/"+e.Entity] = e.Total
	}
	var catalogue struct {
		Resources []catalogueEntry `json:"resources"`
	}
	if err := json.Unmarshal(askWithKey(t, cfg, key, "/api/v1/app/resources"), &catalogue); err != nil {
		t.Fatalf("the catalogue body is not the document: %v", err)
	}
	listed := []string{}
	for _, e := range catalogue.Resources {
		if e.Operations == nil || slices.Contains(e.Operations, "list") {
			listed = append(listed, e.Module+"/"+e.Entity)
		}
	}
	slices.Sort(named)
	slices.Sort(listed)
	return named, totals, listed
}

// TestAKeyScopedToOneReadIsNamedOnlyThatCard primes the store as the administrator,
// then mints two keys from that same administrator: one that reads content and one
// that reads tasks. The content key is named no task card although its holder's
// task count is sitting in the store, and the task key is answered that stored
// count and no card its scope does not read.
func TestAKeyScopedToOneReadIsNamedOnlyThatCard(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	createTask(t, cfg, admin, acmeHost, "counted for the holder", "")
	createTask(t, cfg, admin, acmeHost, "counted again", "")
	_, primed := summarised(t, cfg, admin)
	stored := answered(t, primed["task/task"], "holder's task total")
	if stored != 2 {
		t.Fatalf("two tasks answer the holder a total of %d, want 2", stored)
	}

	for _, tc := range []struct{ scope, reads string }{
		{"content:read", "content/content"},
		{"task:read", "task/task"},
	} {
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
			`{"name":"summary-`+tc.scope+`","scopes":["`+tc.scope+`"]}`)
		if code != http.StatusCreated {
			t.Fatalf("minting a key scoped to %s = %d %s, want 201", tc.scope, code, body)
		}
		named, totals, listed := cardsForKey(t, cfg, field(t, body, "token"))
		if !slices.Contains(named, tc.reads) {
			t.Errorf("a key scoped to %s is not named the %s card it may read: %v", tc.scope, tc.reads, named)
		}
		if !slices.Equal(named, listed) {
			t.Errorf("a key scoped to %s is named %v by the summary and %v by its own catalogue", tc.scope, named, listed)
		}
		if tc.reads != "task/task" && slices.Contains(named, "task/task") {
			t.Errorf("a key scoped to %s is named the task card its holder's session stored", tc.scope)
		}
		if tc.reads == "task/task" {
			if got := answered(t, totals["task/task"], "task key's total"); got != stored {
				t.Errorf("a key scoped to task:read is answered %d tasks, want the %d its holder was", got, stored)
			}
		}
	}
}
