package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// englishSources are the English catalogues the resource names and the admin copy
// added beside their Portuguese files.
var englishSources = []string{
	"modules/admin/messages", "modules/billing/messages", "modules/content/messages",
	"modules/site/messages", "modules/task/messages", "modules/user/messages",
	"ui/resource/messages", "ui/export/messages",
}

// TestEveryEnglishCatalogueCarriesThePortugueseKeys: where a module ships both
// en.json and pt-PT.json (decision 0012), the two files hold the same keys, so the
// English source a pseudo-locale transforms is the whole of what Portuguese says.
func TestEveryEnglishCatalogueCarriesThePortugueseKeys(t *testing.T) {
	for _, dir := range englishSources {
		at := filepath.Join("..", "..", dir)
		en, pt := keysOf(t, filepath.Join(at, "en.json")), keysOf(t, filepath.Join(at, "pt-PT.json"))
		for _, k := range pt {
			if !slices.Contains(en, k) {
				t.Errorf("%s/pt-PT.json has %q and en.json does not", dir, k)
			}
		}
		for _, k := range en {
			if !slices.Contains(pt, k) {
				t.Errorf("%s/en.json has %q and pt-PT.json does not", dir, k)
			}
		}
	}
}

func keysOf(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	keys := make([]string, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	return keys
}
