package main

// The link in the email, followed the way its recipient follows it.
//
// Every other case in this directory that confirms an account reads the
// credential out of the message and posts the JSON door — which is what an SDK
// does. Two things those cases could not see, both measured by a review:
//
//   - the address the mail wrote named a socket nobody was listening on, because
//     the tenant table stores a host *name* (httpx.HostOnly is the loader's key)
//     while the instance answers on a port; and
//   - nothing answered that address at all, because this module registered a JSON
//     door and no page, so the one thing a new person does with the mail landed
//     on the kernel's 404.
//
// The first is the composition's, and it is a pure function of the tenant's host
// and the host the installation answers at, so it is measured as one. The second
// is walked end to end: register, open the exact address the mail carries, press
// the button that page offers, and sign in with the password chosen at the start.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestAnEmailedLinkNamesThePortItsInstanceListensOn(t *testing.T) {
	for _, one := range []struct {
		name, stored, served, want string
	}{
		{"the installation's own host on a port", "localhost", "localhost:8099", "localhost:8099"},
		{"the installation's own host, no port to carry", "localhost", "localhost", "localhost"},
		{"another customer's host, which this process does not answer at",
			"acme.example.com", "localhost:8099", "acme.example.com"},
		{"a host written with the port already on it", "localhost:8099", "localhost:8099", "localhost:8099"},
		{"an installation that says nothing about its host", "acme.example.com", "", "acme.example.com"},
		// The name a tenant was stored under is what the link says, capitals and
		// all: httpx.HostOnly normalises both sides of the comparison, and a link
		// that rewrote the stored name would be a second normalisation.
		{"a name stored with capitals in it still gets its port", "LocalHost", "localhost:8099", "LocalHost:8099"},
	} {
		if got := withServedPort(one.stored, one.served); got != one.want {
			t.Errorf("%s: withServedPort(%q, %q) = %q, want %q", one.name, one.stored, one.served, got, one.want)
		}
	}
}

// TestTheEmailedLinkOpensAPageThatConfirms is the journey in one boot of the
// reference application. The address asked for is the one read out of the
// message — path, query and all — and nothing here posts the JSON door.
func TestTheEmailedLinkOpensAPageThatConfirms(t *testing.T) {
	const (
		address  = "link-journey@acme.test"
		password = "the-link-in-the-mail-is-how-this-account-opens"
	)
	path, cfg := configure(t)
	path, cfg = keepMailInTheProcess(t, path, cfg)
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
	signup, err := json.Marshal(map[string]any{
		"email": address, "displayName": "Link Recipient",
		"password": password, "confirmation": password, "termsAccepted": true,
	})
	if err != nil {
		t.Fatalf("marshal the signup: %v", err)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup = %d %s, want the neutral 202", code, body)
	}

	mail := notificationcontracts.Message{}
	eventually(t, "the verification mail reaches the mailbox", func() bool {
		for _, sent := range box.Sent() {
			if sent.To == address && strings.Contains(sent.Body, contracts.VerifyEmailPath) {
				mail = sent
				return true
			}
		}
		return false
	})
	raw := regexp.MustCompile(`(http://[^\s]+)`).FindString(mail.Body)
	if raw == "" {
		t.Fatalf("the mail carried no address at all; its body read: %s", mail.Body)
	}
	link, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	if link.Host != acmeHost {
		t.Errorf("the link was written for %s, not for the tenant that sent it (%s)", link.Host, acmeHost)
	}
	if link.Path != contracts.VerifyEmailPath {
		t.Errorf("the link aims at %s; this module mails %s", link.Path, contracts.VerifyEmailPath)
	}
	token := link.Query().Get("token")
	if token == "" {
		t.Fatalf("the link carries no credential: %s", raw)
	}

	// 1. GET the address. A page, not a 404 and not a problem document, and it
	// says which address is being confirmed. It consumes nothing: the credential
	// is still live below, and a mail scanner that fetches every link in every
	// message it is handed would otherwise spend the person's one chance.
	status, ctype, page := askNavigate(t, cfg, &http.Client{}, acmeHost, link.RequestURI())
	if status != http.StatusOK {
		t.Fatalf("GET %s = %d (%s), want the confirmation page: %s", link, status, ctype, page)
	}
	if !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Errorf("the confirmation page came as %s; a person navigating gets markup", ctype)
	}
	for _, want := range []string{address, `action="` + contracts.VerifyEmailPath + `"`, "Confirm"} {
		if !strings.Contains(page, want) {
			t.Errorf("the confirmation page does not say %q:\n%s", want, page)
		}
	}

	// 2. Press the button: the form's own submission, posted as a form, not the
	// JSON door beside it.
	code, location, refusal := askForm(t, cfg, &http.Client{}, acmeHost, contracts.VerifyEmailPath, "token="+url.QueryEscape(token))
	if code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d (%s), want the 303 that says it worked", contracts.VerifyEmailPath, code, refusal)
	}
	if location != pinnedSignIn {
		t.Errorf("a confirmed account is sent to %s, not to %s where its password works", location, pinnedSignIn)
	}

	// 3. The password chosen at signup works, which is the only proof that the
	// credential spent itself on the account rather than on the page.
	// What the credential was for: the password the person chose at signup now
	// opens a session, which is the proof that it spent itself on the account.
	signIn(t, cfg, acmeHost, address, password)
}
