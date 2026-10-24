package admin_test

import (
	"encoding/json"
	"os"
	"testing"
)

// TestEveryAdminWorkspaceKeyIsInBothCatalogues checks that the English and
// Portuguese admin catalogues name the same admin.workspace.* keys.
func TestEveryAdminWorkspaceKeyIsInBothCatalogues(t *testing.T) {
	keys := func(file string) map[string]bool {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for id := range doc {
			out[id] = true
		}
		return out
	}
	en, pt := keys("messages/en.json"), keys("messages/pt-PT.json")
	if len(en) == 0 {
		t.Fatal("no keys read from the English catalogue")
	}
	for id := range en {
		if !pt[id] {
			t.Errorf("%s is in en.json and missing from pt-PT.json", id)
		}
	}
}
