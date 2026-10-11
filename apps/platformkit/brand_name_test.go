package main

// The installation's own word for itself, read back off the running installation.
//
// Three surfaces name a workspace and none of them asks the others: the public
// page's header composes its word from modules/web's `name`, the public page's
// footer appends modules/web's `brand`, and this application's machine-readable face
// composes its own through workspaceName and brandName. That is three places spelling
// one name, which is the shape this repository has been caught in before — a comment
// promising a test nobody wrote (review_surfaces_test.go records exactly that). This
// is the test that comment promised, and it asks the question in the only way a test
// in this package can: not whether two constants are equal across a boundary Go will
// not cross (apps/platformkit may not import modules/web/internal), but whether the
// words a person receives at the two surfaces are one word.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

func TestTheInstallationNamesItselfTheSameWayEverywhere(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))

	const tenantName = "Acme Corporation" // what configure() named this tenant

	// Nothing configured: both surfaces fall back one step, to the tenant's name.
	page := publicHome(t, cfg)
	if !strings.Contains(page, ">"+tenantName+"<") {
		t.Fatalf("the public page's header does not print the tenant's own name %q: %s", tenantName, trimHTML(page))
	}
	if name := connectionBody(t, mustBe200(t, cfg, acmeHost))["name"]; name != tenantName {
		t.Errorf("the document names the workspace %v where the page names it %q", name, tenantName)
	}

	// The installation's own word. Only the footer prints it — "<name> · PlatformKit"
	// — and it is printed from modules/web's `brand`, across the boundary this file
	// cannot reach. Read back here, it is the word `brandName` holds: the same word
	// this application's refusal and ask pages wear (faultChrome) and the last name a
	// workspace falls back to. Two spellings of it are a product with two names.
	if !strings.Contains(page, "· "+brandName) {
		t.Errorf("the public page's footer does not print the installation's word %q, which is what this application calls itself everywhere else: %s",
			brandName, trimHTML(page))
	}

	// A title set: the page and the document move together, so a shell reading one
	// and a person reading the other never meet two names for one workspace.
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works"}`); code != http.StatusOK {
		t.Fatalf("setting the title = %d %s", code, body)
	}
	page = publicHome(t, cfg)
	if !strings.Contains(page, ">Acme Works<") {
		t.Errorf("the public page still prints the fallback after the site titled itself: %s", trimHTML(page))
	}
	if name := connectionBody(t, mustBe200(t, cfg, acmeHost))["name"]; name != "Acme Works" {
		t.Errorf("the document names the workspace %v after the page was titled Acme Works", name)
	}
	// The installation's own word survives its workspace's own: the footer is the
	// product, the header is the workspace, and a title never renames the product.
	if !strings.Contains(page, "Acme Works · "+brandName) {
		t.Errorf("the footer stopped printing the installation's word once the workspace had one of its own: %s", trimHTML(page))
	}
}

// publicHome is the page an anonymous visitor of this host is served at its root —
// the browser's side of the face the connection document answers.
func publicHome(t *testing.T, cfg config.Config) string {
	t.Helper()
	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, "/", "")
	if code != http.StatusOK {
		t.Fatalf("a tenant's public home page = %d %s", code, trimHTML(body))
	}
	return body
}
