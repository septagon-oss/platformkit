package main

// The committed composition documents are regenerated wholesale under
// UPDATE_GOLDEN, so the golden comparison alone cannot notice a regeneration
// that quietly resolved to a composition without the approval door. This case
// pins the door's wiring in the document itself: the product provides the
// gate, the first consumer takes it from the product, and the change module
// still receives a subject binding from each of its two contributors.

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestTheResolvedCompositionKeepsTheApprovalDoorWiring(t *testing.T) {
	type edge struct {
		Contract string `json:"contract"`
		From     string `json:"from"`
	}
	type module struct {
		Name     string            `json:"name"`
		Provides []string          `json:"provides"`
		Uses     []edge            `json:"uses"`
		Takes    []json.RawMessage `json:"takes"`
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
		if !slices.Contains(byName["product"].Provides, "rest.Gate") {
			t.Errorf("%s: the product no longer provides rest.Gate, so no module has an approval door", name)
		}
		gateTaken := slices.ContainsFunc(byName["task"].Uses, func(e edge) bool {
			return e.Contract == "rest.Gate" && e.From == "product"
		})
		if !gateTaken {
			t.Errorf("%s: the task module no longer takes rest.Gate from the product, so its writes bypass the door", name)
		}
		contributors := map[string]bool{}
		for _, raw := range byName["change"].Takes {
			var taken struct {
				Contract string   `json:"contract"`
				From     []string `json:"from"`
			}
			if err := json.Unmarshal(raw, &taken); err != nil {
				t.Fatalf("%s: a takes entry of change does not decode: %v", name, err)
			}
			if taken.Contract != "changecontracts.SubjectBinding" {
				continue
			}
			for _, from := range taken.From {
				contributors[from] = true
			}
		}
		for _, contributor := range []string{"product", "access"} {
			if !contributors[contributor] {
				t.Errorf("%s: change no longer takes a changecontracts.SubjectBinding from %s", name, contributor)
			}
		}
	}
}
