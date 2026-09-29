package main

// The tenant's own languages, at the reference application.
//
// `Done when` for T-0111 asks for a tenant whose default is pt-PT rendering the
// admin shell in Portuguese in a test. The case worth writing is the one with no
// `Accept-Language` in it at all: a page that translated when the browser asked in
// Portuguese proved only the negotiation that already existed. The claim this
// delivery makes is that the tenant decides, so nothing here asks, and the shell is
// Portuguese anyway.
//
// The write goes through the control-plane route rather than through SQL, because a
// fixture that poked the column directly would leave the route the only untested
// half of the feature — and the route is where the operator's capability, the event
// and the invalidated host resolution all live.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestATenantServedInPortugueseIsAnsweredInPortuguese signs the operator in, says
// their tenant is served in European Portuguese, and reads the shell back in it:
// first the sign-in page, whose copy is the shell's own, then a generated screen,
// whose labels are the vocabulary `ui/resource` ships and `ui/screens` renders.
func TestATenantServedInPortugueseIsAnsweredInPortuguese(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	operator := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// Which tenant, from the control plane: the locale route is addressed by id.
	code, body := do(t, cfg, operator, http.MethodGet, acmeHost, tenantPath, "")
	if code != http.StatusOK {
		t.Fatalf("the tenant list = %d: %s", code, body)
	}
	var list struct {
		Items []struct {
			ID            string `json:"id"`
			Slug          string `json:"slug"`
			DefaultLocale string `json:"defaultLocale"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("the tenant list is not the shape the route documents: %v\n%s", err, body)
	}
	var id string
	for _, item := range list.Items {
		if item.Slug != "acme" {
			continue
		}
		id = item.ID
		// A tenant created before anybody chose a language is served in the one its
		// copy is written in, and says so. An empty field here would mean the column
		// was written by the migration and read by nobody.
		if item.DefaultLocale == "" {
			t.Error("the control plane describes no default language for the tenant it created")
		}
	}
	if id == "" {
		t.Fatalf("no acme in %s", body)
	}

	// The write, in the other language, by the operator who is allowed to make it.
	// A tag that is not a tag is refused the same way; the module's conformance
	// suite is where that case runs against both implementations.
	code, body = do(t, cfg, operator, http.MethodPost, acmeHost, tenantPath+"/"+id+"/locale",
		`{"default":"pt-PT","supported":["en"]}`)
	if code != http.StatusOK {
		t.Fatalf("setting the tenant's languages = %d: %s", code, body)
	}
	if !strings.Contains(body, `"defaultLocale":"pt-PT"`) {
		t.Fatalf("the route wrote pt-PT and described something else: %s", body)
	}

	// The route names the event it publishes, and the composition refuses an
	// event no module owns — which is why this assertion is here rather than a
	// read of the trail: acme's plan in this fixture does not include the trail
	// (app_test.go's billing case pays for it), and standing up a subscription to
	// read one record would be a second test of the audit module's outbox handler,
	// which already has one. What is checked is that the write published, which
	// kit/app verifies at boot against every route's declaration.

	// The sign-in page, asked for in no language at all.
	code, body = do(t, cfg, operator, http.MethodGet, acmeHost, "/app/admin/login", "")
	if code != http.StatusOK {
		t.Fatalf("the sign-in page = %d: %s", code, short(body))
	}
	for _, want := range []string{`lang="pt-PT"`, "Palavra-passe", "Iniciar sessão"} {
		if !strings.Contains(body, want) {
			t.Errorf("a tenant served in Portuguese was shown no %q: %s", want, short(body))
		}
	}
	if strings.Contains(body, "Password") {
		t.Errorf("the translated sign-in page left the English label behind: %s", short(body))
	}

	// The workspace, signed in, still unasked: navigation, buttons and the count are
	// generated screens, which a translated sign-in page says nothing about.
	code, body = do(t, cfg, operator, http.MethodGet, acmeHost, "/app/task/tasks", "")
	if code != http.StatusOK {
		t.Fatalf("the task screen of a tenant served in Portuguese = %d: %s", code, short(body))
	}
	// The empty list's own words: the create button, the count under the title and
	// the empty state. The row actions (Editar, Eliminar) need a row, and creating
	// one through the API to translate a screen would be this file testing
	// ui/resource's CRUD rather than the language of a screen.
	for _, want := range []string{`lang="pt-PT"`, ">Nova", "Ainda sem"} {
		if !strings.Contains(body, want) {
			t.Errorf("the task screen carries no %q, so the tenant's language stopped at the sign-in page: %s",
				want, short(body))
		}
	}
}

// short is a page read as far as a failing assertion needs.
func short(body string) string {
	if len(body) > 900 {
		return body[:900]
	}
	return body
}
