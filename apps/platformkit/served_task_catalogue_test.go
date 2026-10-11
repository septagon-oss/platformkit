package main

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

func TestTheShippedTaskCatalogueMatchesBothLanguageGoldens(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acmeID := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acmeID, `{"default":"en","supported":["en","pt-PT"]}`)

	for _, language := range []string{"en", "pt-PT", "en"} {
		status, vary, document := catalogueDocument(t, cfg, admin, language)
		if status != http.StatusOK || vary != "Accept-Language" {
			t.Fatalf("catalogue in %s: status %d, Vary %q", language, status, vary)
		}
		resources, ok := document["resources"].([]any)
		if !ok {
			t.Fatal("catalogue has no resources array")
		}
		index := slices.IndexFunc(resources, func(value any) bool {
			entry, ok := value.(map[string]any)
			return ok && entry["module"] == "task" && entry["entity"] == "task"
		})
		if index < 0 {
			t.Fatal("catalogue has no task/task entry")
		}
		golden, err := os.ReadFile("testdata/catalog-task-" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var want any
		if err := json.Unmarshal(golden, &want); err != nil {
			t.Fatal(err)
		}
		gotJSON, err := json.Marshal(resources[index])
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if string(gotJSON) != string(wantJSON) {
			t.Errorf("served task catalogue in %s differs from its golden\ngot: %s\nwant: %s", language, gotJSON, wantJSON)
		}
	}
}
