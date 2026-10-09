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
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestTheSignUpMailsLinkOpensAPage is the brief's fourth decided rule for the
// sign-up mail: "sign-up, resend, invitation and reset each deliver one mail", and
// the link in each is the way on. A person who signs up in this composition (it
// wires EmailRegistration) is mailed a confirmation link; following it in a browser
// has to answer a page that can confirm the address — the page the reset link
// gained in this delivery, for the other mail.
//
// And the link has to lead back to the address the sign-up was answered at, port
// included. The mail is rendered in the worker, after the request is gone, so the
// port only reaches the link if the event carries it: a confirmation built from the
// tenant's name alone opens port 80 on a development installation that serves its
// tenants behind a port, which is the academy bug rule 4 was written against and,
// measured in e2e/mailed-links.spec.ts, the sign-up mail still had while the
// invitation mail did not.
func TestTheSignUpMailsLinkOpensAPage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the link out of it", c.mail)
	}
	_, port, err := net.SplitHostPort(cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	served := acmeHost + ":" + port
	const email = "signs.up@acme.localhost"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "Signs Up", "password": "a password of their own",
		"confirmation": "a password of their own", "termsAccepted": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, served, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup at %s = %d %s, want 202", served, code, body)
	}
	var link string
	eventually(t, "the confirmation link reaches the mailbox", func() bool {
		for _, sent := range box.Sent() {
			if sent.To != email {
				continue
			}
			if m := regexp.MustCompile(`https?://\S+token=\S+`).FindString(sent.Body); m != "" {
				link = m
				return true
			}
		}
		return false
	})
	// The public spelling of the confirmation page, not the workspace one: the link
	// is handed to a person with no session, which is exactly whom the public face
	// answers (modules/auth/internal/ui/page.go, and email_link_page_test.go, which
	// walks this address and presses the button on the page it finds). What this
	// case is about is the host and port in front of that path — the address the
	// sign-up was answered at, which the event carries to the worker that writes the
	// message — and the literal prefix below still insists on both of them.
	if want := "http://" + served + "/auth/verify-email?token="; !strings.HasPrefix(link, want) {
		t.Errorf("the confirmation asked for at %s leads elsewhere: %s, want a link beginning %s", served, link, want)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the mailed link %q: %v", link, err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+u.RequestURI(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host, req.Header["Accept"] = u.Host, []string{"text/html"}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("follow %s: %v", link, err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("the sign-up mail's link %s answers %d; a mailed link opens a page", u.Path, res.StatusCode)
	}
}
