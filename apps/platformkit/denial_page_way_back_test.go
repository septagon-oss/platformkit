package main

// The way on, pinned at the composition that renders it.
//
// A refusal page is the answer a person gets when everything else about the
// application has already said no to them, so the one thing it must not lose is
// somewhere to go. `ui/page`'s refusal renderer draws the way out only when the
// shell handed to `FaultHandler` names one — `document.Refusal` skips the link
// entirely on an empty `Home` — and a shell that reaches that branch with an empty
// `Back` prints a page with a sentence, a stylesheet and no exit: valid markup, a
// 403, a dead end, and nothing in the repository that would notice. `ui/page` checks
// the link for its own demo shell (`fault_test.go` asks for `href="/admin"`);
// nothing checked the one this application boots, where a guard's refusal is rendered
// by `faultPage` in apps/platformkit/fault.go rather than by the admin shell the page
// was refused inside.
//
// So the case asks the shipped composition, as a browser: a person signed in with no
// grant asks for a generated screen (the administrator asking the same address proves
// the address is the screen, and the 403 below is therefore about the person and not
// about a missing route), and the page that answers must carry a link that is a way
// out — and that way out must answer when followed, because a link to an address
// nothing serves is a way on in appearance only. The address it must lead to is the
// dashboard's own, `/app`, which is where this application's workspace begins.
//
// Every assertion reads what the correct page prints: a status, a media type, the
// `href` and its label, and the status of the address the `href` names.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// dashboardAddress is the workspace root, where this composition mounts the dashboard
// (`home.PagePath(dashboardRel)` in modules/admin/internal/mount.go), which is what
// `pinnedHome` in apps/platformkit/fault.go names as the refusal page's way back.
const dashboardAddress = "/app"

const wayBackPassword = "the-way-back-member's-password"

// TestThePageThatRefusesAPersonLeadsThemBackToTheDashboard is the case above.
func TestThePageThatRefusesAPersonLeadsThemBackToTheDashboard(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the confirmation link out of the mailbox it keeps", c.mail)
	}
	email := "way.back.member@acme.test"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "Way Back Member", "password": wayBackPassword,
		"confirmation": wayBackPassword, "termsAccepted": true,
	})
	if err != nil {
		t.Fatalf("marshal the signup: %v", err)
	}
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup = %d %s, want the neutral 202 that activates nothing until the mailbox link is posted", code, body)
	}
	link := ""
	eventually(t, "the confirmation link reaches the mailbox", func() bool {
		for _, sent := range box.Sent() {
			if sent.To != email {
				continue
			}
			if at := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(sent.Body); len(at) == 2 {
				link = at[1]
				return true
			}
		}
		return false
	})
	if code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/verify-email", `{"token":"`+link+`"}`); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("confirming the mailbox link = %d %s, want it to activate the account this composition just accepted", code, body)
	}
	member := signIn(t, cfg, acmeHost, email, wayBackPassword)

	// Reachability, from the other side: the same address, the same Accept header, an
	// administrator. A 403 for the member is then about the grant and nothing else.
	const screen = "/app/task/tasks"
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	if code, _, body := show(t, cfg, admin, acmeHost, screen); code != http.StatusOK {
		t.Fatalf("%s for the administrator = %d, want 200 because this case is about that screen: %s", screen, code, body)
	}
	code, ctype, body := show(t, cfg, member, acmeHost, screen)
	if code != http.StatusForbidden {
		t.Fatalf("%s for a member holding no grant = %d %s, want the verdict 403: %s", screen, code, ctype, body)
	}
	if !strings.Contains(strings.ToLower(body), "<!doctype html>") {
		t.Fatalf("a person who navigated to a screen they may not see was handed %s rather than the shell's page: %s", ctype, body)
	}

	// The way on: a link, with a label, naming an address that is not the screen that
	// refused. An empty href is the defect — the markup stays valid and the person
	// stays put — so the href is read out of the anchor rather than searched for as a
	// substring, which a `href=""` beside a correct link would have satisfied.
	href := ""
	label := ""
	for _, anchor := range regexp.MustCompile(`(?s)<a\b[^>]*href="([^"]*)"[^>]*>(.*?)</a>`).FindAllStringSubmatch(body, -1) {
		text := strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(anchor[2], ""))
		if text == "" || strings.Contains(text, "app.css") {
			continue
		}
		href, label = anchor[1], text
		break
	}
	if href == "" || label == "" {
		t.Fatalf("the 403 page this composition shows offers no way out (no anchor with a label): %s", body)
	}
	if href == screen || href == "" {
		t.Errorf("the way out of the refusal is the refusal itself (%q): the page leads the person back to the screen that refused them", href)
	}
	if href != dashboardAddress {
		t.Errorf("the refusal page leads to %q, not to %s, the dashboard this application's workspace begins at", href, dashboardAddress)
	}

	// And the address it leads to answers for the person standing on the refusal.
	if back, _, backBody := show(t, cfg, member, acmeHost, href); back != http.StatusOK {
		t.Errorf("following the way out (%q, labelled %q) = %d, want the dashboard to answer: %s", href, label, back, backBody)
	}
}
