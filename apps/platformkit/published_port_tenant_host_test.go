package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestAPublishedPortKeepsTheRecipientsTenantHostDespiteACallersPort(t *testing.T) {
	path, cfg, relay := configureMailing(t)
	install(t, path)
	// The installation supplies only the port. Its hostname is not the
	// recipient's tenant, and the request supplies neither part of the link.
	cfg.Server.PublicHost = "installation.localhost:8443"
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	// The relay is this installation's mail server: what it kept is the link.
	box := relay
	code, body := do(t, cfg, nil, http.MethodPost, acmeHost+":7",
		"/api/v1/public/auth/password/forgot", `{"email":"`+adminEmail+`"}`)
	if code != http.StatusOK {
		t.Fatalf("request reset = %d %s, want 200", code, body)
	}
	var mail string
	eventually(t, "the recipient's reset mail", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == adminEmail && strings.Contains(sent.Body, "token=") {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if want := "http://" + acmeHost + ":8443/app/auth/reset?token="; !strings.Contains(mail, want) {
		t.Errorf("reset mail must keep the tenant's host and the declared public port; want %s in:\n%s", want, mail)
	}
}
