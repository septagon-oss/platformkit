package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestTenantLanguageCommandCanBeInvokedFromItsCatalog(t *testing.T) {
	cfg, client := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	code, body := do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("catalog status=%d: %s", code, body)
	}
	var catalog struct {
		Resources []struct {
			Module   string `json:"module"`
			Commands []struct {
				Verb   string `json:"verb"`
				Path   string `json:"path"`
				Method string `json:"method"`
			} `json:"commands"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, resource := range catalog.Resources {
		if resource.Module != "tenant" {
			continue
		}
		for _, command := range resource.Commands {
			if command.Verb != "languages" {
				continue
			}
			if command.Path == "" {
				t.Fatal("tenant language command has no explicit endpoint")
			}
			code, body = do(t, cfg, client, http.MethodGet, acmeHost, command.Path, "")
			var current struct {
				Revision int64 `json:"revision"`
			}
			if code != http.StatusOK {
				t.Fatalf("read command's resource=%d: %s", code, body)
			}
			if err := json.Unmarshal([]byte(body), &current); err != nil {
				t.Fatal(err)
			}
			// Catalog v1 commands use POST unless the document declares another method.
			// Follow the published path, without knowledge of tenant's implementation.
			method := command.Method
			if method == "" {
				method = http.MethodPost
			}
			payload := fmt.Sprintf(`{"default":"pt-PT","supported":["en"],"expectedRevision":%d}`, current.Revision)
			code, body = do(t, cfg, client, method, acmeHost, command.Path, payload)
			if code != http.StatusOK {
				t.Errorf("catalog command %s %s = %d: %s; the catalogue must describe an executable command", method, command.Path, code, body)
			}
			return
		}
	}
	t.Fatal("tenant language command is missing from the catalog")
}
