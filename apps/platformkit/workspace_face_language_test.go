package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
)

// TestTheShippedWorkspaceMountServesTheCatalogueInTheRequestsLanguage boots the
// options the product itself boots with — the catalogue and the connection
// document mounted together through workspaceFace — rather than start's default
// of the catalogue alone, and asks both: the connection document still answers
// before sign-in, and the catalogue beside it still negotiates Accept-Language,
// naming the header in Vary.
func TestTheShippedWorkspaceMountServesTheCatalogueInTheRequestsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	if code, body := do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, ""); code != http.StatusOK {
		t.Fatalf("GET %s with no session = %d %s: the shared mount lost the connection document", connectionPath, code, body)
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acmeID := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acmeID, `{"default":"en","supported":["en","pt-PT"]}`)
	for _, want := range []struct{ accept, singular string }{
		{accept: "pt-PT", singular: "tarefa"},
		{accept: "en", singular: "task"},
	} {
		status, vary, doc := catalogueDocument(t, cfg, admin, want.accept)
		if status != http.StatusOK {
			t.Fatalf("GET /api/v1/app/resources with Accept-Language %s = %d", want.accept, status)
		}
		if vary != "Accept-Language" {
			t.Errorf("the shipped mount answered Vary %q, want Accept-Language", vary)
		}
		if got := taskSingular(t, doc); got != want.singular {
			t.Errorf("the shipped mount asked in %s calls a task %q, want %q", want.accept, got, want.singular)
		}
	}
}
