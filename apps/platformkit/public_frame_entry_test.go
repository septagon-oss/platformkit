package main

// The one address an anonymous visitor is offered from a tenant's public page,
// asked of the running application.
//
// apps/platformkit/modules.go hands the public site a single way into the
// workspace (web.Deps.SignInPath) and says why the composition, and not the
// module, writes it. What nothing said until CI refused the head this branch was
// pinned at is which address that was: it was the sign-in page two segments deep,
// so the frame a visitor reads on the tenant's own host linked deeper into the
// workspace than its root. e2e/public-frame-links-workspace-root.spec.ts reads
// the page in a browser and is the delivery's gate; this is the same invariant
// where make check can reach it, because the browser job is not the only place a
// composition may be checked, and a rewired Deps should fail here first.
//
// The case reaches the answer the way a visitor does: through the public page
// and the root it links, never through the deeper address the invariant forbids,
// so a rewired Deps fails by moving the address a person is actually offered.

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// anchorHref is the address on an <a>, which is what a visitor can follow. The
// same attribute on a <link> is a stylesheet and leads nowhere a person goes.
var anchorHref = regexp.MustCompile(`<a [^>]*href="([^"]+)"`)

// TestThePublicFrameLinksOnlyTheWorkspaceRoot: the public frame leads into the
// workspace at its root and nowhere deeper. The page must offer an entry — a
// frame with no way in is the same failure wearing a different hat — and every
// address it offers into the workspace must be the root the composition pins.
func TestThePublicFrameLinksOnlyTheWorkspaceRoot(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	code, page := do(t, cfg, nil, http.MethodGet, acmeHost, "/", "")
	if code != http.StatusOK {
		t.Fatalf("a fresh tenant's public home page = %d %s", code, trimHTML(page))
	}
	entries := frameEntries(page)
	if len(entries) == 0 {
		t.Fatalf("the public home page offers an anonymous visitor no way into the workspace: %s", trimHTML(page))
	}
	for _, href := range entries {
		if href != pinnedWorkspace {
			t.Errorf("the public frame links %s, want only %s: a page an anonymous caller reads leads into "+
				"the workspace at its root and no deeper, because the root is the address that answers whoever "+
				"arrives and decides what they need next", href, pinnedWorkspace)
		}
	}
	// The root the frame links is a door and not a dead end. Asked the way a
	// browser asks, it arrives at the form the shell is named for — which is what
	// makes linking the root rather than the page honest: the visitor who wants to
	// sign in is sent to the sign-in, and the person who already has a session
	// lands in the workspace.
	if code, form := navigate(t, cfg, acmeHost, pinnedWorkspace); code != http.StatusOK ||
		!strings.Contains(form, "data-login-form") {
		t.Errorf("following the frame's entry %s = %d, want the sign-in form the workspace gives an anonymous "+
			"browser: %s", pinnedWorkspace, code, trimHTML(form))
	}
}

// frameEntries is every address on the page a visitor could follow into the
// workspace — the browser's a[href^="/app"], spelled with the pin that composes
// the prefix so a moved root moves the case with it.
func frameEntries(page string) []string {
	var entries []string
	for _, match := range anchorHref.FindAllStringSubmatch(page, -1) {
		if strings.HasPrefix(match[1], pinnedWorkspace) {
			entries = append(entries, match[1])
		}
	}
	return entries
}

// navigate is a navigation: a GET that says it is a browser, and follows where
// the answer sends it. The address a guarded page gives a caller that does not
// say what it is (see do) is the JSON refusal, not the page.
func navigate(t *testing.T, cfg config.Config, host, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}
