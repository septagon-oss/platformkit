package history_test

// The series a record answers at its own address is part of the same API as the
// record: every other body the kit publishes spells its fields the way the schema
// does — id, createdAt, revision — and the panel, the mobile counterpart and anything
// a client writes against the OpenAPI document read the version page by those names.
// A page that spelled its items with Go's exported names would be a second contract
// published beside the first, and one that breaks every client the day it is fixed.

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAVersionsPageSpellsItsFieldsAsTheRestOfTheApiDoes(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	memo := create(t, router, acmeHost, "First draft")
	edit(t, router, acmeHost, memo, "Second draft")

	code, body := call(t, router, acmeHost, http.MethodGet, address(memo)+"/versions", "")
	if code != http.StatusOK {
		t.Fatalf("GET the versions = %d %s", code, body)
	}
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the versions page does not decode: %v — %s", err, body)
	}
	if len(page.Items) != 2 {
		t.Fatalf("the page answered %d versions, want 2: %s", len(page.Items), body)
	}
	item := page.Items[0]
	// The names the panel reads; module, entity and record are the address the page
	// was asked at, and whether an item repeats them is the fixer's call.
	for _, name := range []string{"id", "locale", "version", "fields", "changed", "reason", "actor", "at"} {
		if _, ok := item[name]; !ok {
			t.Errorf("a version on the page has no %q, the spelling the record's own body uses: %s", name, body)
		}
	}
	for _, name := range []string{"ID", "Module", "Version", "Fields", "Actor", "At"} {
		if _, ok := item[name]; ok {
			t.Errorf("a version on the page carries %q, a Go field name, beside the record's camelCase body: %s", name, body)
		}
	}
	var version int64
	if err := json.Unmarshal(item["version"], &version); err != nil || version != 2 {
		t.Errorf("the newest version on the page is %s, want 2 under \"version\"", item["version"])
	}
}
