package main

import (
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// The unit of this — which address reads as a port and which reads as none — is
// TestPublishedPortReadsAPortAndNothingElse in modules/tenant: the function moved
// there with the host lookup that calls it, and a case stays with its subject.

// A caller may write a Host header that names no port at all, and on an
// installation that declares nothing that reads as the scheme's default. An
// installation that declares its public port is not left to a caller's omission:
// the link a person is mailed leads to the address the installation says it is
// reached at, whether or not the request that raised it named that port, and
// whichever port the process itself happens to answer on.
func TestAPublicPortAnInstallationDeclaresOutlivesARequestThatNamesNoPort(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)

	_, published, err := net.SplitHostPort(freeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	_, served, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	if published == served {
		t.Fatalf("the published port and the served port are both %s, so this case asks nothing", published)
	}
	cfg.Server.PublicHost = acmeHost + ":" + published

	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the link out of it", c.mail)
	}
	// The header names acme and no port: what a proxy that terminates on 443
	// forwards, and what a caller who would move a link onto another port writes
	// when they leave the port out.
	code, body := do(t, cfg, nil, http.MethodPost, acmeHost,
		"/api/v1/public/auth/password/forgot", `{"email":"`+adminEmail+`"}`)
	if code != http.StatusOK {
		t.Fatalf("asking for a link at %s = %d %s, want 200", acmeHost, code, body)
	}
	var mail string
	eventually(t, "the reset link to be mailed", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == adminEmail && strings.Contains(sent.Body, "token=") {
				mail = sent.Body
				return true
			}
		}
		return false
	})
	if want := "http://" + acmeHost + ":" + published + "/app/auth/reset?token="; !strings.Contains(mail, want) {
		t.Errorf("this installation declares %s, and a request that named no port was answered with a link elsewhere:\n%s",
			acmeHost+":"+published, mail)
	}
}
