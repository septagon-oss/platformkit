package main

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAPublicMissingPageLinksOnlyTheWorkspaceRoot: a page an anonymous visitor
// asks of a tenant's public site that does not exist is still a public page, so
// every address it offers into the workspace is the root the composition pins,
// as on the home page (decision 0020).
func TestAPublicMissingPageLinksOnlyTheWorkspaceRoot(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	code, page := navigate(t, cfg, acmeHost, "/no-such-page")
	if code != http.StatusNotFound {
		t.Fatalf("an unpublished slug on the public site = %d, want 404: %s", code, trimHTML(page))
	}
	for _, href := range frameEntries(page) {
		if href != pinnedWorkspace {
			t.Errorf("the public missing page links %s, want only %s", href, pinnedWorkspace)
		}
	}
}
