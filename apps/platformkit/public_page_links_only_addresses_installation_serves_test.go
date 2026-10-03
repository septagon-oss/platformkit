package main

// The reviewer's own cases for T-0024 against the reference composition. Each
// asserts something the change promises in a comment or in a commit body, and has
// a branch that passes once the promise is kept. Where a case's reachability
// matters it is proved with what correct behaviour prints (a status, the door that
// answers, the page the settings name), never with the defect's own output.
//
// Reviewer: a fresh pi session, 2026-09-21. REVIEW.md in the state directory
// carries the reproduction of each and the assertion it falsifies.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestThePublicPageLinksOnlyAddressesTheInstallationServes.
//
// apps/platformkit/modules.go hands the public site its two foreign addresses and
// says of them: "A module that named them would be naming a surface it does not
// serve — see web.Deps, and TestPublicSiteLinks, which asks the running server
// that both answers." There is no test of that name anywhere in this repository
// (`grep -rn TestPublicSiteLinks .` finds the comment and nothing else), and one
// of the two addresses the comment vouches for answers nothing.
//
// `PublicFileURL` builds "/files/<id>". The file module's public door is a route
// on the public *value* surface, so it composes to
// /api/v1/public/file/files/<id>; the bare "/files/<id>" is a *document* address,
// and the module that claims the public root answers documents at "/{slug}" — one
// segment. A two-segment address matches nothing at all. So every tenant that sets
// a logo publishes a broken image on the one page its own customers see. On main
// the address was spelled inside the module and was right
// (`filePath = "/api/v1/file/public/"`); moving it into the composition moved it
// wrong, and the test the comment names as its pin was never written.
//
// The passing branch: name the address the file module composes (its own
// Router.Path), and this case asks the running server for the bytes.
func TestThePublicPageLinksOnlyAddressesTheInstallationServes(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A real file, stored public, so the correct address answers with bytes rather
	// than with a refusal this case would have to argue about.
	const svg = "<svg xmlns='http://www.w3.org/2000/svg'/>"
	code, body := putFile(t, cfg, admin, filesPath+"?visibility=public", "logo.svg", "image/svg+xml", svg)
	if code != http.StatusCreated {
		t.Fatalf("uploading the logo = %d %s; this case cannot be asked without a file", code, body)
	}
	logo := field(t, body, "id")

	// Reachability, from what the correct behaviour prints: the door the kernel
	// composes for a public file serves those bytes to an anonymous visitor at the
	// tenant's own host. True today and after any fix.
	if code, got := do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/public/file/files/"+logo, ""); code != 200 || !strings.Contains(got, "<svg") {
		t.Fatalf("the public file door answered %d %s; the case needs a file a visitor can reach", code, trimHTML(got))
	}

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, "/api/v1/site/settings",
		`{"logoFileId":"`+logo+`","title":"Acme"}`); code != http.StatusOK {
		t.Fatalf("setting the logo = %d %s; this case cannot be asked without one", code, body)
	}
	code, page := do(t, cfg, nil, http.MethodGet, acmeHost, "/", "")
	if code != http.StatusOK {
		t.Fatalf("a tenant's public home page = %d %s", code, trimHTML(page))
	}
	at := imgSrc(page)
	if !strings.Contains(at, logo) {
		t.Fatalf("the public page rendered no logo for %s (src=%q): %s", logo, at, trimHTML(page))
	}

	// The assertion: an address the public page links is an address the
	// installation serves.
	if code, got := do(t, cfg, nil, http.MethodGet, acmeHost, at, ""); code != 200 || !strings.Contains(got, "<svg") {
		t.Errorf("the public page links the tenant's logo as %s and the installation answers %d (%s); "+
			"the file module's public door composes to /api/v1/public/file/files/{id}, so web.Deps.PublicFileURL "+
			"in the composition names an address nothing serves — and TestPublicSiteLinks, the test "+
			"apps/platformkit/modules.go names as the pin for exactly this pair, does not exist",
			at, code, trimHTML(got))
	}
}

// imgSrc is the src of the first img on the page, which is the logo: the header
// renders one only when the site settings name a file.
func imgSrc(page string) string {
	i := strings.Index(page, "<img")
	if i < 0 {
		return ""
	}
	tag := page[i:]
	if end := strings.Index(tag, ">"); end > 0 {
		tag = tag[:end]
	}
	const key = `src="`
	j := strings.Index(tag, key)
	if j < 0 {
		return ""
	}
	rest := tag[j+len(key):]
	if k := strings.Index(rest, `"`); k >= 0 {
		return rest[:k]
	}
	return rest
}

// TestTheCatalogNamesWhereTheWritesOfAResourceAre is decision 0019 at the
// catalog's own boundary: a client that is not a browser builds its whole
// vocabulary out of this document, so every write it says the caller may do has to
// come with the address of that write.
//
// This change added `command.path` for exactly that reason — "A command the
// installation owns lives on the control plane, whose address does not start from
// the resource's own, so POST {Path}/{id}/{verb} would send a caller to an address
// nothing answers at" — and applied it to the lifecycle commands only. The five
// CRUD writes of a resource whose reads and writes stand on different surfaces are
// still derived from the entry's own path, and for the worked case the code itself
// names (modules/billing's price list: readable on the workspace surface at
// /api/v1/billing/plans, writable only on the control plane at
// /api/v1/ops/billing/plans) the derived address takes a GET and refuses every
// method that changes anything.
//
// The passing branch is either half of the same sentence: the entry names an
// address that answers the write, the way a command now does; or the entry does not
// tell this caller the resource is writable from this surface.
func TestTheCatalogNamesWhereTheWritesOfAResourceAre(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, who, http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("the workspace catalog = %d %s", code, body)
	}
	var doc struct {
		Resources []struct {
			Path     string `json:"path"`
			Writable bool   `json:"writable"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("the catalog is not the document a shell parses: %v\n%s", err, body)
	}

	// Reachability first, and none of it is the defect's own output: find a
	// resource this caller may write whose write door really is on the control
	// plane. True today and after any fix.
	var asked bool
	for _, r := range doc.Resources {
		if !r.Writable {
			continue
		}
		ops := "/api/v1/ops" + strings.TrimPrefix(r.Path, "/api/v1")
		if c, _ := do(t, cfg, who, http.MethodPost, acmeHost, ops, `{"currency":"EUR"}`); c == http.StatusNotFound || c == http.StatusMethodNotAllowed {
			continue // this resource's writes are not on the control plane
		}
		asked = true
		// A shell derives the create door from the entry's own path. Ask the
		// installation whether that door exists, and ask the document whether it
		// said the write is somewhere else — which is all `command.path` says.
		if c, _ := do(t, cfg, who, http.MethodPost, acmeHost, r.Path, `{"currency":"EUR"}`); c == http.StatusNotFound || c == http.StatusMethodNotAllowed {
			t.Errorf("the catalog says this caller may write %s and gives no address for it: POST %s = %d, "+
				"and the entry names no other address. command.path was added for this sentence and stops at "+
				"the lifecycle verbs, so a native client cannot create or edit the price list at all",
				r.Path, r.Path, c)
		}
	}
	if !asked {
		t.Fatal("no writable resource of this composition has its writes on the control plane; " +
			"the case was written against the split and must move with it")
	}
}

// TestEveryAddressAShippedExampleNavigatesToIsStillServed.
//
// ui/components/examples/gallery.go is the component reference the foundation ships,
// served to a signed-in person at /app/admin/_gallery, and its examples navigate:
// `Href: "/admin"`, `Href: "/admin/_gallery"`, and
// `components.LinkProps{Label: "New tenant", Href: "/admin/tenants/new"}`. This change
// retired the `/admin` namespace and wrote a table saying where its old addresses go.
// The table matches "an exact address or a path segment prefix", so
// `/admin/tenants/new` is redirected by its general row to `/app/tenants/new` — and
// nothing is mounted there, because the module is named `tenant` and the entity
// `tenants`: a generated screen composes /app/tenant/tenants. The link the component
// reference prints is therefore a redirect into a 404, which is the failure
// TestTheOldAddressesOfTheReferenceApp names in its own comment as worse than the 404
// a person would have been given, "because the redirect makes it look as though the
// installation broke rather than as though the link aged".
//
// TestEveryNavEntryLeadsSomewhere asks the same question of a module manifest's
// navigation, and the gallery is not navigation.
//
// The case takes the retired addresses out of the shipped file, so that correcting an
// example — to an address that answers, or to none at all — is enough to pass it. It
// proves the machinery is live first: /admin, the general row's own subject, is shown
// answering something other than a 404.
func TestEveryAddressAShippedExampleNavigatesToIsStillServed(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	src, err := os.ReadFile("../../ui/components/examples/gallery.go")
	if err != nil {
		t.Fatalf("read the shipped component reference: %v", err)
	}
	var retired []string
	for _, m := range navLiteral.FindAllStringSubmatch(string(src), -1) {
		if strings.HasPrefix(m[1], "/admin") {
			retired = append(retired, m[1])
		}
	}
	if len(retired) == 0 {
		t.Fatal("the shipped examples navigate to no /admin address any more; read the file and update this case")
	}

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	for _, href := range retired {
		status, rest := followed(t, cfg, admin, href)
		if status == http.StatusNotFound {
			t.Errorf("an example the foundation ships navigates to %s, which comes to rest at %s and answers 404; "+
				"the alias table redirects it there on purpose, so the installation looks broken", href, rest)
		}
	}
}

// navLiteral is an address one of the shipped examples navigates to: a link's own
// href, or the hx-get a boosted button asks for.
var navLiteral = regexp.MustCompile(`(?:Href:|Get:|SettleGet:|PopGet:)\s*"(/[a-z0-9_/.-]+)"`)

// followed asks for an address the way a browser asks for a bookmarked link — it
// takes each redirect in turn, carrying the session cookie — and reports the status
// it came to rest on and the address it came to rest at.
func followed(t *testing.T, cfg config.Config, client *http.Client, href string) (int, string) {
	t.Helper()
	watch := *client
	watch.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	at := href
	for range 4 {
		req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+at, nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Host = acmeHost
		res, err := watch.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", at, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode < http.StatusMultipleChoices || res.StatusCode >= 400 {
			return res.StatusCode, at
		}
		target := res.Header.Get("Location")
		if target == "" {
			t.Fatalf("%s answered %d with nowhere to go: %s", at, res.StatusCode, body)
		}
		if !strings.HasPrefix(target, "http") {
			at = target
			continue
		}
		u, err := url.Parse(target)
		if err != nil {
			t.Fatalf("parse %s: %v", target, err)
		}
		at = u.Path
	}
	t.Fatalf("%s did not come to rest inside four redirects", href)
	return 0, ""
}
