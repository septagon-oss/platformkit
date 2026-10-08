package main

// The catalog's version rule, written down as a gate. ui/screens.CatalogVersion
// is the one number in this repository that the person changing it does not get
// to choose, and the rule beside it — additive, optional, never a change of
// meaning — protects the build already installed in a pocket. `operations` is
// the first key whose *absence* and whose *presence* mean different things to a
// shell that has not read it, so the moment a shipped resource hides a verb, a
// build in a pocket would draw a New button against an address that refuses it.
// This file refuses that moment rather than remembering it.
//
// The moment ends where the document says it ends. A shell reads
// `catalogVersion` and the version that makes `operations` required reading is
// the change that moves this rule's ground, so the rule is stated with that
// number in it: refusing a verb-hiding entry below it, and refusing a *raise*
// that changed nothing. What keeps the raise honest is not this file: it is
// scripts/check_public_api.py, which refuses a moved CatalogVersion until the
// reviewed baseline is rewritten by hand in its own build(api) commit.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// operationsRequiredFrom is the document version from which an entry may name
// fewer than the five operations: the version whose parser reads `operations`
// rather than deriving the five, and so the version at which hiding one stops
// being a surprise to a build that has not read the key. ui/screens/catalog.go
// states the same number in words beside CatalogVersion; this is the house's
// "the same number, read twice". It is not this file's to choose — see the
// apidiff gate above, which is what makes moving the other half an act somebody
// reviewed rather than a memory.
const operationsRequiredFrom = 3

// promisesFive reports whether a document at this version still promises the five
// CRUD routes to a shell that derives them. That is the whole condition the rule
// below turns on, written as a function so the case can ask it of the document it
// is reading and of the two versions either side of the line.
func promisesFive(version int) bool { return version < operationsRequiredFrom }

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
	// Both directions of the version, the two above being both directions of the
	// predicate. A rule no raise can satisfy is not a rule — it is a ban on the
	// shape, and the shape is rest.Spec's own documented answer to a resource
	// whose writes are commands. A rule that fires on no version is a check that
	// has stopped checking.
	if !promisesFive(operationsRequiredFrom - 1) {
		t.Errorf("a version %d document admitted an entry that hides a verb, which a shell written against five derived buttons would draw a door for",
			operationsRequiredFrom-1)
	}
	if promisesFive(operationsRequiredFrom) {
		t.Errorf("raising the version to %d answered nothing: the rule refused an entry that hides a verb whatever the document says",
			operationsRequiredFrom)
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
		if hidesAVerb(e) && promisesFive(catalog.Version) {
			t.Errorf("%s/%s publishes operations %v while CatalogVersion is %d, which promises five routes to a build that has not been told otherwise: raise the version in this change, and the shell's parser with it",
				e.Module, e.Entity, e.Operations, screens.CatalogVersion)
		}
	}
}
