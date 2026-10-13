package main

// The catalogue's language claim, pinned where it is served rather than where it
// is built: GET /api/v1/app/resources negotiates Accept-Language through the
// tenant's declared set, the same expression the page beside it uses. The unit
// cases in reference_reading_test.go select a locale by hand and so cannot say
// whether the route's own context carries the request and the tenant; this one
// sends the header over the wire and reads the words that come back.

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestTheCatalogueAnswersInTheRequestsLanguage declares a tenant served in both
// languages, asks its catalogue twice with two Accept-Language headers, and reads
// the task entry's own singular: "tarefa" for the Portuguese request, "task" for
// the English one, with Vary naming the header that chose.
func TestTheCatalogueAnswersInTheRequestsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
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
			t.Errorf("the catalogue answered Vary %q, want Accept-Language: a negotiated body must name what chose it", vary)
		}
		if got := taskSingular(t, doc); got != want.singular {
			t.Errorf("the catalogue asked in %s says a task is %q, want %q: the route is not negotiating the request's language", want.accept, got, want.singular)
		}
	}
}

// catalogueDocument is one GET of the document with one Accept-Language line,
// through the signed-in client's own cookies.
func catalogueDocument(t *testing.T, cfg config.Config, client *http.Client, accept string) (int, string, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+"/api/v1/app/resources", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept-Language", accept)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/app/resources: %v", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the document: %v", err)
	}
	doc := map[string]any{}
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("the document is not its documented shape: %v\n%s", err, body)
		}
	}
	return res.StatusCode, res.Header.Get("Vary"), doc
}

// taskSingular is what the document calls one task.
func taskSingular(t *testing.T, doc map[string]any) string {
	t.Helper()
	resources, _ := doc["resources"].([]any)
	for _, entry := range resources {
		e, _ := entry.(map[string]any)
		if e["module"] == "task" && e["entity"] == "task" {
			presentation, _ := e["presentation"].(map[string]any)
			if presentation == nil {
				t.Fatal("the task entry carries no presentation block")
			}
			singular, _ := presentation["singular"].(string)
			return singular
		}
	}
	t.Fatal("the document carries no task/task entry")
	return ""
}
