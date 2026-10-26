package main

// The guided session reaches a shell through two keys that sit beside shapes that
// shell already parses, so the promise is the one a presentation key keeps in
// presentation_contract_test.go: nothing that a document without a session says
// may turn into something a shipped shell refuses. The served document is where
// that promise is written down — `check-apidiff` compares exported Go and names no
// served wire field — and the golden's own case compares bytes, so a re-record that
// followed a key made required would be recorded as fact rather than refused here.

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// sessionContractKeys are the two shapes a shell reads and the key the guided
// session arrived beside on each. The key must be published, or a shell has no way
// to find the read contract or the resolved session at all; it must stay optional,
// or every resource that is no guided session — all but one of this document — reads
// as a broken response to a shell that never asked for a session.
var sessionContractKeys = map[string]string{
	"Entry":    "readContract",
	"Resolved": "session",
}

func TestTheServedDocumentPublishesTheSessionContractWithoutRequiringIt(t *testing.T) {
	raw, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	components := wireMap(wireMap(wireMap(doc["components"])["schemas"]))
	for shape, key := range sessionContractKeys {
		component := wireMap(components[shape])
		if _, ok := wireMap(component["properties"])[key]; !ok {
			t.Errorf("%q publishes no %q, so a shell reading the catalogue cannot tell a guided session from any other row", shape, key)
			continue
		}
		if slices.Contains(wireStringList(component["required"]), key) {
			t.Errorf("%q requires %q on the wire: a resource that is no guided session writes neither, so the document would describe a response that no answer satisfies", shape, key)
		}
	}
}
