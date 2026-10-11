package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// Seeding the starter through the command must make the public home show its
// page. A published content row alone is not enough when the site still has no
// home slug: the visitor reaches /, not the admin's content API.
func TestSeededTenantOpensOnItsHomePage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	if err := seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail}); err != nil {
		t.Fatalf("seed the tenant: %v", err)
	}
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{
		Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans, Authenticate: c.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
	})
	code, body := do(t, cfg, nil, http.MethodGet, acmeHost, "/", "")
	if code != http.StatusOK {
		t.Fatalf("public home returned %d; want 200 with starter content: %s", code, body)
	}
	if !strings.Contains(body, "This site is served by PlatformKit") {
		t.Errorf("public home does not show its seeded page; response includes empty state: %t", strings.Contains(body, "Nothing published yet"))
	}
}
