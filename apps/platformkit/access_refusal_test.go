package main

// The brief's own journey, walked at the reference application end to end:
// a person with no grant is refused a screen, the refusal names what is
// missing in the defining module's words and points at a role rather than a
// person, the ask behind it tells the holders of role management in this
// tenant and only them, the grant happens through the link the notice carries,
// and the retry that follows succeeds.
//
// Every earlier refusal test in this directory asks one part — the language
// (review_r6, review_r7, review_round4), the shape of the problem document,
// the host and tenant resolution. None walked the whole page: none signed the
// refused person back in after somebody granted what the page had named.

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

const (
	askDeniedScreen   = "/app/task/tasks"
	askMemberEmail    = "refused@acme.test"
	askMemberPassword = "a-person-who-was-refused-chooses-this"
	askGranterEmail   = "root@globex.localhost"
)

// TestARefusedPersonSeesWhatIsMissingWhoCanGrantItAsksAndIsGranted walks the
// journey named above in one boot of the reference application.
func TestARefusedPersonSeesWhatIsMissingWhoCanGrantItAsksAndIsGranted(t *testing.T) {
	// The mailbox sink, asked for by name: this case reads a confirmation link out
	// of this process's memory, which is what mail.sink: mailbox is for. Without it
	// the composition has no mail at all and registration answers 503.
	path, cfg := configure(t)
	path, cfg = keepMailInTheProcess(t, path, cfg)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	// A second tenant with its own administrator, so "the neighbours were not
	// told" is an assertion with a neighbour in it rather than an absence.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, globexID, askGranterEmail)
	globexAdmin := signIn(t, cfg, globexHost, askGranterEmail, adminPass)

	// The refused person: signed up through the public door at acme, confirmed
	// with the mailbox link this composition kept, signed in. They hold the
	// tenant's member role, which the seed grants nothing.
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the confirmation link out of it", c.mail)
	}
	signup, err := json.Marshal(map[string]any{
		"email": askMemberEmail, "displayName": "Refused Member",
		"password": askMemberPassword, "confirmation": askMemberPassword, "termsAccepted": true,
	})
	if err != nil {
		t.Fatalf("marshal the signup: %v", err)
	}
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup)); code != http.StatusAccepted {
		t.Fatalf("signup = %d %s, want the neutral 202", code, body)
	}
	link := ""
	eventually(t, "the confirmation link reaches the mailbox", func() bool {
		for _, sent := range box.Sent() {
			if sent.To != askMemberEmail {
				continue
			}
			if at := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(sent.Body); len(at) == 2 {
				link = at[1]
				return true
			}
		}
		return false
	})
	if code, body = do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/verify-email", `{"token":"`+link+`"}`); code != http.StatusOK && code != http.StatusAccepted {
		t.Fatalf("confirming the mailbox link = %d %s", code, body)
	}
	member := signIn(t, cfg, acmeHost, askMemberEmail, askMemberPassword)
	memberID := userIDOf(t, cfg, askMemberEmail)

	// 1. The refusal page. A person navigating is refused a page, and the page
	// carries its four parts: the verdict, the grant in words, a role and not a
	// list of people, and a way on that is not the address that refused.
	status, ctype, html := askNavigate(t, cfg, member, acmeHost, askDeniedScreen)
	if status != http.StatusForbidden {
		t.Fatalf("GET %s for a person with no grant = %d (%s), want 403: %s", askDeniedScreen, status, ctype, firstLineOf(html))
	}
	if !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Fatalf("a navigating caller was answered %s, so no page was rendered: %s", ctype, firstLineOf(html))
	}
	if want := "What is missing: read tasks"; !strings.Contains(html, want) {
		t.Errorf("the refusal does not name the grant in the defining module's words (%q): %s", want, firstLineOf(html))
	}
	if want := "anyone whose role grants them manage roles"; !strings.Contains(html, want) {
		t.Errorf("the refusal does not name a role as the granter (%q): %s", want, firstLineOf(html))
	}
	for _, name := range []string{adminEmail, askGranterEmail} {
		if strings.Contains(html, name) {
			t.Errorf("the refusal names a person (%s); it may name a role and nothing else: %s", name, firstLineOf(html))
		}
	}
	if !strings.Contains(html, `href="`+pinnedHome+`"`) {
		t.Errorf("the refusal offers no way on (%s): %s", pinnedHome, firstLineOf(html))
	}
	// And the way on is asked of the running server as this person, not as a
	// string in a page: an address that answers 404 is the dead end this brief
	// exists to close, and TestPinnedAddresses asks its pins anonymously, where the
	// answer at the workspace root is a redirect and not a page.
	if code, ctype, page := askNavigate(t, cfg, member, acmeHost, pinnedHome); code != http.StatusOK ||
		!strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Errorf("the refusal's way on %s = %d %s for the person it refused, want the workspace home: %s",
			pinnedHome, code, ctype, firstLineOf(page))
	}
	if strings.Contains(html, `href="`+askDeniedScreen+`"`) {
		t.Errorf("the refusal links back to the address that refused: %s", firstLineOf(html))
	}
	if m := regexp.MustCompile(`name="permission"[^>]*value="task:read"`).FindString(html); m == "" {
		t.Errorf("the ask control does not carry the refused grant: %s", firstLineOf(html))
	}
	if !strings.Contains(html, `action="/app/access-request"`) {
		t.Errorf("the refusal mounts no ask control posting to /app/access-request: %s", firstLineOf(html))
	}

	// 2. The ask, submitted the way the page submits it: the form behind the
	// button, both hidden fields as the refusal wrote them, posted once.
	code, location, posted := askForm(t, cfg, member, acmeHost, "/app/access-request",
		"permission=task%3Aread&path=%2Fapp%2Ftask%2Ftasks")
	if code != http.StatusSeeOther {
		t.Fatalf("POST /app/access-request = %d %s, want the 303 to the confirmation", code, posted)
	}
	if want := "/app/access-request/sent"; location != want {
		t.Errorf("the ask lands at %q, want %q", location, want)
	}
	if status, _, html = askNavigate(t, cfg, member, acmeHost, "/app/access-request/sent"); status != http.StatusOK ||
		!strings.Contains(html, "Your request was sent") {
		t.Errorf("the confirmation page = %d: %s", status, firstLineOf(html))
	}

	// 3. Exactly one notice, to the holders of role management in the refused
	// request's own tenant. Acme's administrator sees one ask naming the
	// person and carrying the deep link; Globex's administrator, who also holds
	// role management somewhere, sees nothing at all.
	eventually(t, "the ask reaches acme's administrator's bell, once", func() bool {
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
		return code == http.StatusOK && strings.Count(body, "Access requested") == 1
	})
	code, body = do(t, cfg, admin, http.MethodGet, acmeHost, noticePath, "")
	if code != http.StatusOK {
		t.Fatalf("GET %s as acme's administrator = %d %s", noticePath, code, body)
	}
	if !strings.Contains(body, memberID.String()) || !strings.Contains(body, "task:read") {
		t.Errorf("the notice names neither the asker nor the grant: %s", body)
	}
	if !strings.Contains(body, "/app/user/users/"+memberID.String()) {
		t.Errorf("the notice carries no deep link to the person's own screen: %s", body)
	}
	if code, body = do(t, cfg, globexAdmin, http.MethodGet, globexHost, noticePath, ""); code != http.StatusOK ||
		strings.Contains(body, "asked for") || strings.Contains(body, memberID.String()) {
		t.Errorf("Globex's administrator was told about an ask in acme: %d %s", code, body)
	}

	// The ask is in the trail with the notices it wrote — same transaction, so
	// by the time the bell shows it, the event is readable.
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var notified, permission string
	eventually(t, "security.access_requested reaches the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT coalesce(max(payload->>'notified'), ''), coalesce(max(payload->>'permission'), '')
			   FROM audit_events WHERE name = 'security.access_requested'`).Scan(&notified, &permission)
		return err == nil && notified != "" && permission == "task:read"
	})
	if notified != "1" {
		t.Errorf("security.access_requested recorded notified=%s, want the one notice that was written", notified)
	}

	// 4. The grant, through the link the notice carried: the administrator
	// opens the person's generated screen and posts the role there. The deep
	// link is an address, not a token — it grants nothing by being visited.
	code, body = askNavigateCode(t, cfg, admin, "/app/user/users/"+memberID.String())
	if code != http.StatusOK {
		t.Fatalf("the notice's deep link = %d: %s", code, firstLineOf(body))
	}
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost,
		"/api/v1/user/users/"+memberID.String()+"/roles", `{"roles":["admin"]}`); code != http.StatusOK {
		t.Fatalf("POST roles as acme's administrator = %d %s, want 200", code, body)
	}

	// 5. The retry succeeds. The same address, the same session, a page now.
	status, ctype, html = askNavigate(t, cfg, member, acmeHost, askDeniedScreen)
	if status != http.StatusOK {
		t.Fatalf("the retry of %s after the grant = %d (%s), want the screen itself: %s",
			askDeniedScreen, status, ctype, firstLineOf(html))
	}
}

// askNavigate asks one page address with the caller's session and answers the
// verdict, media type and body.
func askNavigate(t *testing.T, cfg config.Config, client *http.Client, host, path string) (int, string, string) {
	t.Helper()
	code, ctype, body := askNavigateRaw(t, cfg, client, http.MethodGet, host, path, "", "")
	return code, ctype, body
}

func askNavigateCode(t *testing.T, cfg config.Config, client *http.Client, path string) (int, string) {
	t.Helper()
	code, _, body := askNavigateRaw(t, cfg, client, http.MethodGet, acmeHost, path, "", "")
	return code, body
}

// askForm posts the refusal-page form and answers the verdict and the
// Location, without following the redirect: the 303 and its target are the
// answer, and following it would ask the same command twice.
func askForm(t *testing.T, cfg config.Config, client *http.Client, host, path, form string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+cfg.Server.Addr+path, strings.NewReader(form))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noRedirect := &http.Client{
		Jar: client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header.Get("Location"), firstLineOf(string(body))
}

func askNavigateRaw(t *testing.T, cfg config.Config, client *http.Client, method, host, path, contentType, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), string(out)
}

// userIDOf reads the id the person's rows carry, from the owner's connection —
// the same raw read the audit assertion makes. The notice must name the person
// by that id, and only the deep link's shape says whose screen it opens.
func userIDOf(t *testing.T, cfg config.Config, email string) uuid.UUID {
	t.Helper()
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var id uuid.UUID
	if err := owner.QueryRowContext(t.Context(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("look up %s: %v", email, err)
	}
	return id
}
