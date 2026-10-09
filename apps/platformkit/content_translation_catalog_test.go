package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestContentTranslationsAreReachableFromTheReferenceCatalog(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport, options.Log = memory.New(), quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, who, http.MethodGet, acmeHost, "/api/v1/admin/resources", "")
	if code != http.StatusOK {
		t.Fatalf("catalog = %d %s", code, body)
	}
	var catalog struct {
		Resources []struct {
			Module, Entity string
			Fields         []struct {
				Name         string
				Translatable bool
			}
			Commands []struct{ Verb string }
		}
	}
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, resource := range catalog.Resources {
		if resource.Module != "content" || resource.Entity != "content" {
			continue
		}
		fields := map[string]bool{}
		for _, field := range resource.Fields {
			fields[field.Name] = field.Translatable
		}
		for _, name := range []string{"title", "body"} {
			if !fields[name] {
				t.Errorf("content %s is not translatable in the reference catalog", name)
			}
		}
		commands := map[string]bool{}
		for _, command := range resource.Commands {
			commands[command.Verb] = true
		}
		for _, verb := range []string{"translate", "review-translation", "suggest-translation", "untranslate"} {
			if !commands[verb] {
				t.Errorf("reference content has no %s command", verb)
			}
		}
		return
	}
	t.Fatal("reference catalog has no content resource")
}
