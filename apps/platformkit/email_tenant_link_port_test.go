package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestADevelopmentTenantsEmailLinkNamesTheListeningPort(t *testing.T) {
	path, cfg, relay := configureMailing(t)
	install(t, path)
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	// The installation has its own host; the same process serves acme.localhost
	// at this port as well. The tenant host must remain the link's hostname.
	cfg.Server.PublicHost = "platformkit.localhost:" + port
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	const address = "tenant-link-port@acme.test"
	const password = "the-tenant-mail-link-must-reach-this-port"
	body, err := json.Marshal(map[string]any{
		"email": address, "displayName": "Tenant Link",
		"password": password, "confirmation": password, "termsAccepted": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if status, answer := do(t, cfg, nil, http.MethodPost, acmeHost,
		"/api/v1/public/auth/register", string(body)); status != http.StatusAccepted {
		t.Fatalf("registration = %d %s", status, answer)
	}

	var mail string
	eventually(t, "the tenant's verification email arrives", func() bool {
		for _, sent := range relay.Sent() {
			if sent.To == address && strings.Contains(sent.Body, contracts.VerifyEmailPath) {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	raw := regexp.MustCompile(`http://[^\s]+`).FindString(mail)
	if raw == "" {
		t.Fatal("the verification mail contains no link")
	}
	link, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if link.Hostname() != acmeHost || link.Path != contracts.VerifyEmailPath {
		t.Fatalf("the emailed link names host %q and path %q, want this tenant's verification page",
			link.Hostname(), link.Path)
	}
	if link.Port() != port {
		t.Errorf("the emailed link names %q while the tenant is served on port %q", link.Host, port)
	}
}
