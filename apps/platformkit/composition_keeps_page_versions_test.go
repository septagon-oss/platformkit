package main

// Decision 0069 §6 and the brief for the history module settle that the reference
// application keeps a version of every page: the history module is composed, and the
// content module records through the httpx.Versions port it provides. The committed
// composition documents are the one place that wiring is visible without booting, so
// this case reads them: a history module that is written but composed by nothing keeps
// no version of anything, and a content module that does not take the port is the
// "no versions by design" comment standing after the module that retires it exists.

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestTheResolvedCompositionKeepsAVersionOfEveryPage(t *testing.T) {
	type edge struct {
		Contract string `json:"contract"`
		From     string `json:"from"`
	}
	type module struct {
		Name     string   `json:"name"`
		Provides []string `json:"provides"`
		Needs    []edge   `json:"needs"`
	}
	for _, name := range []string{"COMPOSITION.development.json", "COMPOSITION.production.json"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%s is not committed: %v", name, err)
		}
		var doc struct {
			Modules []module `json:"modules"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s does not decode: %v", name, err)
		}
		byName := map[string]module{}
		for _, m := range doc.Modules {
			byName[m.Name] = m
		}
		history, composed := byName["history"]
		if !composed {
			t.Errorf("%s: no module named history is composed, so no record in the reference application keeps a version", name)
			continue
		}
		if !slices.Contains(history.Provides, "httpx.Versions") {
			t.Errorf("%s: the history module provides %v and not httpx.Versions, so no resource can record through it", name, history.Provides)
		}
		takesVersions := slices.ContainsFunc(byName["content"].Needs, func(e edge) bool {
			return e.Contract == "httpx.Versions" && e.From == "history"
		})
		if !takesVersions {
			t.Errorf("%s: the content module does not take httpx.Versions from history, so a page's writes keep no version and nothing can be restored", name)
		}
	}
}
