package main

// The catalog's version rule, written down as a gate. ui/screens.CatalogVersion
// is the one number in this repository that the person changing it does not get
// to choose, and the rule beside it — additive, optional, never a change of
// meaning — protects the build already installed in a pocket. `operations` is
// the first key whose *absence* and whose *presence* mean different things to a
// shell that has not read it, so the moment a shipped resource hides a verb, a
// build in a pocket would draw a New button against an address that refuses it.
// This file refuses that moment rather than remembering it.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// TestTheCatalogVersionNamesEveryResourceThatHidAVerb holds the version at the
// document it describes: the reference composition may publish an entry that
// hides a verb only in the same change that raises CatalogVersion, which is the
// change the shell's own parser has to be written for.
func TestTheCatalogVersionNamesEveryResourceThatHidAVerb(t *testing.T) {
	// hidesAVerb is the predicate the rule turns on: an entry naming fewer
	// operations than the five a shell would otherwise derive. An entry naming
	// none is not hiding anything — it is the entry that means all five, and it
	// is printed that way so the document reads exactly as it did before the key
	// existed.
	hidesAVerb := func(e screens.Entry) bool {
		return len(e.Operations) > 0 && len(e.Operations) < len(httpx.CRUDValues())
	}
	if hidesAVerb(screens.Entry{}) {
		t.Error("an entry naming no operations is read as hiding none, so the rule refused the wrong entry")
	}
	if !hidesAVerb(screens.Entry{Operations: []string{"list", "read"}}) {
		t.Error("an entry that hides create, update and delete did not refuse a version that still promises all five")
	}

	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("GET the catalog = %d %s", code, body)
	}
	var catalog screens.Catalog
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatalf("the catalog the composition serves does not parse: %v", err)
	}
	if catalog.Version != screens.CatalogVersion {
		t.Fatalf("the served document is version %d and the code says %d: the same number, read twice",
			catalog.Version, screens.CatalogVersion)
	}
	for _, e := range catalog.Resources {
		if hidesAVerb(e) {
			t.Errorf("%s/%s publishes operations %v while CatalogVersion is %d, which promises five routes to a build that has not been told otherwise: raise the version in this change, and the shell's parser with it",
				e.Module, e.Entity, e.Operations, screens.CatalogVersion)
		}
	}
}
