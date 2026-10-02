package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
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
	const email = "signs.up@acme.localhost"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "Signs Up", "password": "a password of their own",
		"confirmation": "a password of their own", "termsAccepted": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup = %d %s, want 202", code, body)
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
