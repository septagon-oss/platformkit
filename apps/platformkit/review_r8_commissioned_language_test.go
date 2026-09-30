package main

// Review round 8's pin, at the reference application: the ceiling on a tenant's
// languages is wired, not merely available.
//
// `modules/tenant` refuses a language it has no copy for by checking the request
// against `Deps.Languages`, and the composition supplies that field from the
// catalogues it read (`apps/platformkit/modules.go`: `Languages:
// installed.Languages()`). The rule itself is proven in both implementations by
// `tenanttest`'s conformance case, and the bootstrap's own refusal is pinned at the
// app by round 2's file. What nothing pinned is the *route*: an operator's POST to
// `/api/v1/ops/tenant/tenants/{id}/locale` at a composition that stopped handing the
// service its languages would answer `200`, write `default_locale = 'de'`, and leave
// every test in the repository green — because `spoken()` returns nil when the
// composition named nothing, which is the module's documented freedom ("A composition
// that wires no catalogues passes nothing, and then nothing is checked against it").
// The page a person then opens declares `lang="de"` and shows English: a browser, a
// screen reader and a translation tool are told a language the deployment cannot
// speak.
//
// This file asks the route, at the composition a deployment is built from. Its
// reachability control is the same route accepting a language the installation does
// carry — a 200 with the tag echoed back — so the refusals below are reached through
// what the fixed behaviour prints, never through what a broken one prints.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestALocaleRouteRefusesALanguageTheInstallationHasNoCopyFor asks the operator's
// locale route, in one process, for the two shapes of one fault: an unsupported
// default, and an unsupported entry in the supported set.
func TestALocaleRouteRefusesALanguageTheInstallationHasNoCopyFor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	operator := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := tenantID(t, cfg, operator, "acme")

	// Control: the route is served, the actor is granted, and a language this
	// installation carries is written. Everything below is reached through this.
	code, body := do(t, cfg, operator, http.MethodPost, acmeHost, tenantPath+"/"+id+"/locale",
		`{"default":"pt-PT","supported":["en"]}`)
	if code != http.StatusOK || !strings.Contains(body, `"defaultLocale":"pt-PT"`) {
		t.Fatalf("the control write of a carried language = %d: %s", code, body)
	}

	// The two refusals. `de` names no file any catalogue in this composition reads.
	for _, c := range []struct{ name, body string }{
		{"an unsupported default", `{"default":"de","supported":["pt-PT"]}`},
		{"an unsupported entry in the set", `{"default":"pt-PT","supported":["de"]}`},
	} {
		code, body := do(t, cfg, operator, http.MethodPost, acmeHost, tenantPath+"/"+id+"/locale", c.body)
		if code == http.StatusOK {
			t.Errorf("%s was accepted: the tenant is now served in a language no catalogue "+
				"carries, so its pages will declare that language and show another: %s", c.name, short(body))
			continue
		}
		if !strings.Contains(body, "de") {
			t.Errorf("%s was refused with a reason naming nothing: %d %s", c.name, code, short(body))
		}
	}

	// The refused writes changed nothing: the declaration the control wrote is still
	// the declaration, and the shell is still worded in it. A refused mutation that
	// returned a stale row or moved the page is the same fault as the accepted one.
	code, body = do(t, cfg, operator, http.MethodGet, acmeHost, tenantPath, "")
	if code != http.StatusOK {
		t.Fatalf("the tenant list = %d: %s", code, body)
	}
	var list struct {
		Items []struct {
			Slug          string `json:"slug"`
			DefaultLocale string `json:"defaultLocale"`
			Locales       []string
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("the tenant list is not the shape the route documents: %v\n%s", err, body)
	}
	for _, item := range list.Items {
		if item.Slug != "acme" {
			continue
		}
		if item.DefaultLocale != "pt-PT" {
			t.Errorf("a refused write moved the tenant's default language to %q", item.DefaultLocale)
		}
		for _, tag := range item.Locales {
			if tag == "de" {
				t.Error("a refused write put a language with no catalogue into the tenant's set")
			}
		}
	}

	code, body = do(t, cfg, operator, http.MethodGet, acmeHost, "/app/admin/login", "")
	if code != http.StatusOK {
		t.Fatalf("the sign-in page = %d: %s", code, short(body))
	}
	if !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("the shell of a tenant served in Portuguese lost its language to a refused write: %s", short(body))
	}
	if strings.Contains(body, `lang="de"`) {
		t.Error("the shell declares German, a language this installation has no copy for")
	}
}

// tenantID reads one tenant's id off the control plane's own list.
func tenantID(t *testing.T, cfg config.Config, admin *http.Client, slug string) string {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, tenantPath, "")
	if code != http.StatusOK {
		t.Fatalf("the tenant list = %d: %s", code, body)
	}
	var list struct {
		Items []struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &list); err != nil {
		t.Fatalf("the tenant list is not the shape the route documents: %v\n%s", err, body)
	}
	for _, item := range list.Items {
		if item.Slug == slug {
			return item.ID
		}
	}
	t.Fatalf("no %q tenant in %s", slug, body)
	return ""
}
