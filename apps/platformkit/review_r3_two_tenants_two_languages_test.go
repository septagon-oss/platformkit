package main

// A reviewer's case, T-0111 review round 3 — the pillar contract's line 1 asked for
// by name: "Name the test that fails when a second tenant's data, event, key,
// locale, sender or cache entry is reachable from the first."
//
// Nothing in this tree answers that for *locale*. The tenant cases in this package
// (`tenant_locale_test.go`) declare one tenant's language and read it back at one
// host; `ui/page/tenant_locale_test.go` negotiates over a `tenancy.Tenant` a test
// handed it, never over two resolutions in one process; and the host cache — the one
// place a language could cross a tenant, because it is keyed by host and holds the
// `Languages` the tenant declared — is only ever observed with one tenant behind it.
// So the claim this pins is the one the whole feature rests on: in the single process
// decision 0028 shares between customers, the language of a page is resolved per
// request from the tenant that request belongs to, and the neighbour's declaration is
// neither readable nor reachable.
//
// Two tenants, one installation, one running application, one browser header sent at
// both hosts. The header names the language the *other* tenant declared, so an answer
// that crossed over would be indistinguishable from a negotiation that simply
// ignored the tenant — which is why the same string is sent at both addresses rather
// than the language each tenant serves. The `?lang=` leg is the brief's invariant
// again through the door the product installed: `apps/platformkit/locale.go` lets an
// explicit query value outrank the browser, and it must not let it outrank the tenant.
//
// Every assertion reads what the correct behaviour prints — the declared `lang`
// attribute and the catalogue's own sentence in that language — never an error
// sentence or a redirect this branch might or might not emit.

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// getLanguage is a navigating GET: the one header a browser brings and nothing else.
// `do` in app_test.go sets a JSON content type and takes no header, which is right
// for an API call and wrong for a page.
func getLanguage(t *testing.T, cfg config.Config, host, path, accept string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	if accept != "" {
		req.Header.Set("Accept-Language", accept)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s at %s: %v", path, host, err)
	}
	return res.StatusCode, string(body)
}

// declareLocale says which languages one tenant is served in, at the address the
// control plane is served at, by the operator the bootstrap created.
func declareLocale(t *testing.T, cfg config.Config, admin *http.Client, id uuid.UUID, body string) {
	t.Helper()
	code, out := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id.String()+"/locale", body)
	if code != http.StatusOK {
		t.Fatalf("POST %s/%s/locale = %d: %s", tenantPath, id, code, out)
	}
}

// TestTwoTenantsOfOneInstallationAreServedInTwoLanguagesAtOnce.
func TestTwoTenantsOfOneInstallationAreServedInTwoLanguagesAtOnce(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))

	// The two declarations, made to disagree: one tenant served only in Portuguese,
	// its neighbour only in English, both written by the same operator at the same
	// moment through the same route.
	acmeID := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acmeID, `{"default":"pt-PT","supported":[]}`)
	declareLocale(t, cfg, admin, globexID, `{"default":"en","supported":[]}`)

	// One header, sent at both hosts. It names English, which is what the second
	// tenant serves and the first refuses, and no Portuguese at all, which is what
	// the first serves and the second refuses. Whichever way a resolution leaked,
	// one of the two answers below would come out the other tenant's.
	const header = "en-GB,en;q=0.9"
	// `foreignKey` is asked for at the English tenant only, and the asymmetry is
	// deliberate rather than an oversight this file forgot: English words do appear on
	// the Portuguese tenant's page — ui/document's anonymous-session banner
	// ("Sign-in required", "Sign in (opens a new tab)") is a literal no catalogue
	// carries — which is the pseudo-locale literal gate this branch names as unshipped
	// (ui/page/README.md; IMPLEMENT's item 2) and not one tenant's copy reaching
	// another's request. The direction that would prove a leak is the other one: a
	// sentence that exists only in the neighbour's declaration showing up here.
	for _, want := range []struct {
		host       string
		declared   string
		ownCopy    string
		foreignKey string
	}{
		{host: acmeHost, declared: `lang="pt-PT"`, ownCopy: "Iniciar sessão"},
		{host: globexHost, declared: `lang="en"`, ownCopy: "Sign in", foreignKey: "Iniciar sessão"},
	} {
		status, html := getLanguage(t, cfg, want.host, "/app/admin/login", header)
		if status != http.StatusOK {
			t.Fatalf("GET /app/admin/login at %s = %d, want 200: the sign-in page is the page a person with "+
				"no session has, and a refusal here would answer the question about nothing but the refusal",
				want.host, status)
		}
		if !strings.Contains(html, want.declared) {
			t.Errorf("the sign-in page at %s declared no %s while this tenant is served in that language alone: %s",
				want.host, want.declared, firstLineOf(html))
		}
		if !strings.Contains(html, want.ownCopy) {
			t.Errorf("the sign-in page at %s carries no %q, so this request was not answered from the set this tenant declared: %s",
				want.host, want.ownCopy, firstLineOf(html))
		}
		if want.foreignKey != "" && strings.Contains(html, want.foreignKey) {
			t.Errorf("the sign-in page at %s carries %q, the other tenant's copy: %s",
				want.host, want.foreignKey, firstLineOf(html))
		}
	}

	// The product's own override, at the tenant that refuses English.
	// `?lang=en` is a real door (apps/platformkit/locale.go) and the e2e spec uses
	// it in the other direction; what no test says is that it stops at the tenant's
	// set rather than at the catalog's.
	status, html := getLanguage(t, cfg, acmeHost, "/app/admin/login?lang=en", header)
	if status != http.StatusOK {
		t.Fatalf("GET /app/admin/login?lang=en at %s = %d", acmeHost, status)
	}
	if !strings.Contains(html, `lang="pt-PT"`) || !strings.Contains(html, "Iniciar sessão") {
		t.Errorf("a URL asked %s for English and the page obeyed the URL over the tenant: %s",
			acmeHost, firstLineOf(html))
	}
}

// installationTenantID reads the id of a tenant by slug off the control plane's own
// list, which is the only way a test learns it that is not a SQL statement.
func installationTenantID(t *testing.T, cfg config.Config, admin *http.Client, slug string) uuid.UUID {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, tenantPath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", tenantPath, code, body)
	}
	var list struct {
		Items []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("the tenant list is not its documented shape: %v\n%s", err, body)
	}
	slugs := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Slug == slug {
			return uuid.MustParse(item.ID)
		}
		slugs = append(slugs, item.Slug)
	}
	t.Fatalf("no tenant with slug %q among %v", slug, slugs)
	return uuid.Nil
}

// firstLineOf is a page read as far as a failing assertion needs, with the markup
// that says which language the document declared kept where it is visible.
func firstLineOf(html string) string {
	if len(html) > 700 {
		return html[:700]
	}
	return html
}
