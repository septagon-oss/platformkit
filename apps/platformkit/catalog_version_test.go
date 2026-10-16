package main

// The catalog's version rule, written down as a gate. ui/screens.CatalogVersion
// is the one number in this repository that the person changing it does not get
// to choose, and the rule beside it — additive, optional, never a change of
// meaning — protects the build already installed in a pocket. `operations` is the
// first key whose *absence* and whose *presence* mean different things to a shell
// that has not read it: an entry that names none means all five, and an entry that
// names four means four.
//
// Where that can strand a pocket is one entry, not the document. A shell draws a
// button per entry it was given, so an entry naming fewer verbs than five strands a
// build only if that build had already been shown the entry — and a build shown the
// entry was shown the verbs it names, which are in the same document. A resource
// new to the catalogue is therefore additive whatever operations it names, and this
// rule says so: `webhook/endpoint` names no create (its answer is a signing secret,
// a row's shape and not the entity's) and `webhook/delivery` names no write at all
// (a delivery is a fact), and no installed build has a button of theirs to draw.
//
// What does strand one is a listed resource losing a verb: the build already drew
// that button and gets no word that the address stopped answering. So the rule
// turns on a record of the verbs each listed resource had — recorded, like the
// public-API baseline, by the person raising the version rather than by the tree
// that drifted from it — and refuses a loss while the version still says the record
// is current.

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// shippedVerbsPath records the verbs the resources of this document offered at the
// catalog version the record names. It is a reviewed baseline, not a golden: nothing
// regenerates it, because a file the tree rewrote could not refuse the tree. It is
// re-recorded, by hand, in the change that raises CatalogVersion — the same change
// scripts/check_public_api.py refuses to let rewrite its own baseline unreviewed.
const shippedVerbsPath = "testdata/catalog-verbs-shipped.json"

// shippedVerbs is that record: the version it was taken at, and the operations each
// listed resource offered, keyed by "module/entity".
type shippedVerbs struct {
	Version   int                 `json:"catalogVersion"`
	Resources map[string][]string `json:"resources"`
}

// lostVerbs is the verbs the entry offered and no longer offers. An empty recorded
// list means a resource nothing was told about, which can lose nothing; an entry
// that names no operations offers all five, which is what every recorded verb of
// version 2 was.
func lostVerbs(recorded, served []string) []string {
	var lost []string
	for _, v := range recorded {
		if !slices.Contains(served, v) {
			lost = append(lost, v)
		}
	}
	return lost
}

// TestTheCatalogVersionNamesEveryResourceThatHidAVerb asks the served document, of
// every resource the record lists, whether a verb it had is gone — the one change to
// `operations` a build in a pocket cannot survive, and the only one that has to be
// paid for by raising the version the shell reads.
func TestTheCatalogVersionNamesEveryResourceThatHidAVerb(t *testing.T) {
	raw, err := os.ReadFile(shippedVerbsPath)
	if err != nil {
		t.Fatalf("read the record of shipped verbs: %v", err)
	}
	var shipped shippedVerbs
	if err := json.Unmarshal(raw, &shipped); err != nil {
		t.Fatalf("%s does not parse: %v", shippedVerbsPath, err)
	}
	if len(shipped.Resources) == 0 {
		t.Fatalf("%s records no resource, which lets any verb be hidden from it", shippedVerbsPath)
	}

	five := make([]string, 0, len(httpx.CRUDValues()))
	for _, c := range httpx.CRUDValues() {
		five = append(five, string(c))
	}
	// All five words, and a verb outside them is a word no shell can read: the
	// record's own vocabulary, refused here rather than in the next delivery's
	// silent mismatch, where a typo would match nothing and refuse nothing.
	for key, verbs := range shipped.Resources {
		for _, v := range verbs {
			if !slices.Contains(five, v) {
				t.Errorf("%s records the operation %q, which is not one of %v the shell knows", key, v, five)
			}
		}
	}

	// The rule in both directions, so that a change which made it answer nothing on
	// every document — the shape of a check that stopped checking — is visible here.
	if got := lostVerbs(five, []string{"list", "read", "update", "delete"}); !slices.Equal(got, []string{"create"}) {
		t.Errorf("a listed resource left without create named the loss %v, want [create]", got)
	}
	if got := lostVerbs(nil, []string{"list", "read"}); got != nil {
		t.Errorf("a resource no installed build has been shown named the loss %v, want none", got)
	}
	if got := lostVerbs(five, five); got != nil {
		t.Errorf("a resource that still offers every verb it offered named the loss %v, want none", got)
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
	if shipped.Version != screens.CatalogVersion {
		t.Errorf("%s was taken at catalog version %d and the code serves %d: the raise that changed the promise re-records it, in the change that moves the number",
			shippedVerbsPath, shipped.Version, screens.CatalogVersion)
	}

	served := map[string][]string{}
	for _, e := range catalog.Resources {
		verbs := e.Operations
		if len(verbs) == 0 {
			verbs = five
		}
		for _, v := range e.Operations {
			if !slices.Contains(five, v) {
				t.Errorf("%s/%s publishes the operation %q, which is not one of %v the shell can read",
					e.Module, e.Entity, v, five)
			}
		}
		served[e.Module+"/"+e.Entity] = verbs
		recorded, listed := shipped.Resources[e.Module+"/"+e.Entity]
		if !listed {
			// New to the document: an entry no installed build has drawn has no
			// button to lose, so its verb set is its author's to declare.
			continue
		}
		if lost := lostVerbs(recorded, verbs); lost != nil {
			t.Errorf("%s/%s no longer offers %v, which the record lists at catalog version %d: a build that drew that button is still drawing it, so raise CatalogVersion in this change and re-record %s in the same one",
				e.Module, e.Entity, lost, shipped.Version, shippedVerbsPath)
		}
	}
	for key := range shipped.Resources {
		if _, ok := served[key]; !ok {
			t.Errorf("%s is in the shipped record and not in the served document, which takes every verb it had from a build that drew them: raise CatalogVersion in this change and re-record %s in the same one",
				key, shippedVerbsPath)
		}
	}
}
