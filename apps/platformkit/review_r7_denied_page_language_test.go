package main

// A reviewer's case, T-0111 review round 7 (2026-09-30).
//
// Round 6 closed its HIGH — the language of a refusal answered ahead of routing ignored the
// tenant's declared languages — and its own "Unverified" section names the half it could not
// reach: "the 403 and the 503 rendered as a page, in a tenant's language". It tried a CSRF
// refusal (a POST to a page is a 405) and a problem document on /api/ (a code, no language).
// The 403 that *is* reachable was in reach all along and nobody asked it: `review_round4_denied
// _page_test.go`, a file this repository already owns, signs up a member who holds no grant and
// is shown the shell's page at /app/task/tasks. Round 4 proved the shape; no case since has
// asked that page which language it is in.
//
// That refusal reaches the renderer through `(*API).refuse` — inside the huma chain, with a
// resolved tenant, an open transaction and a session — and so it takes the *other* branch of
// the cure in `kit/httpx` than the 405 does: `withHostTenant` is supposed to find a tenant
// already on the request and resolve nothing. If that branch is wrong, the person is refused in
// whichever language their browser brought, at a tenant that declared one language, and every
// guard refusal inside the chain shares it.
//
// Reachability runs first and through the fixed behaviour only: the administrator, same header,
// same address, gets the screen in the tenant's declared language. The case then asks the member
// the same address, and reaches the assertion through the verdict (403) and the media type
// (a page), never through what a wrong-language answer would print.

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// r7DeniedScreen is a screen the tenant's ordinary member role holds nothing about, and
// r7MemberPassword is their own chosen secret.
const (
	r7DeniedScreen   = "/app/task/tasks"
	r7MemberPassword = "a-member-with-no-grant-chooses-this"
)

// r7navigate asks one address the way a browser does, with the caller's own language ranking,
// and answers the verdict, the media type, Content-Language and the body.
func r7navigate(t *testing.T, cfg config.Config, client *http.Client, host, path, ranking string) (int, string, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", ranking)
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s at %s: %v", path, host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s at %s: %v", path, host, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Language"), string(body)
}

// TestAPersonRefusedAScreenIsAnsweredInTheTenantsLanguage — the 403 a signed-in person is
// shown, at a tenant that declared Portuguese alone, asked in English.
func TestAPersonRefusedAScreenIsAnsweredInTheTenantsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	acme := installationTenantID(t, cfg, admin, "acme")
	declareLocale(t, cfg, admin, acme, `{"default":"pt-PT","supported":[]}`)

	// The refused person: signup through the public door, confirmed with the mailbox link this
	// composition kept, signed in. They hold the tenant's member role, which the seed grants
	// nothing (modules/auth/internal/seed.go), and so are refused every generated screen.
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the reference application mails through %T; this case reads the confirmation link out of it", c.mail)
	}
	email := "r7.member@acme.test"
	signup, err := json.Marshal(map[string]any{
		"email": email, "displayName": "R7 Member",
		"password": r7MemberPassword, "confirmation": r7MemberPassword, "termsAccepted": true,
	})
	if err != nil {
		t.Fatalf("marshal the signup: %v", err)
	}
	code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/public/auth/register", string(signup))
	if code != http.StatusAccepted {
		t.Fatalf("signup at the reference application = %d %s, want the neutral 202", code, body)
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
	confirmMailboxLink(t, cfg, acmeHost, link)
	member := signIn(t, cfg, acmeHost, email, r7MemberPassword)

	// The caller's ranking names only English, at a tenant served in Portuguese alone. Every
	// assertion below reaches the refusal through its verdict and media type — what the fixed
	// behaviour prints — and then asks which language that page claims.
	const ranking = "en-GB,en;q=0.9"

	// Control, and it is the tenant boundary on a page: the administrator, same address, same
	// ranking, is served the screen, in the tenant's language and not the browser's. If this
	// fails, the two cases below mean nothing.
	status, ctype, contentLang, html := r7navigate(t, cfg, admin, acmeHost, r7DeniedScreen, ranking)
	if status != http.StatusOK {
		t.Fatalf("GET %s for the administrator = %d (%s): %s", r7DeniedScreen, status, ctype, firstLineOf(html))
	}
	if contentLang != "pt-PT" || !strings.Contains(html, `lang="pt-PT"`) {
		t.Fatalf("the fixture's own screen at %s is not served in pt-PT (Content-Language %q): %s",
			acmeHost, contentLang, firstLineOf(html))
	}

	// The person with no grant, at the same address with the same ranking: a page, and the
	// tenant's language — the sentence ui/page/messages/pt-PT.json carries for AUTH_DENIED.
	status, ctype, contentLang, html = r7navigate(t, cfg, member, acmeHost, r7DeniedScreen, ranking)
	if status != http.StatusForbidden {
		t.Fatalf("GET %s for a member holding no grant = %d (%s), want 403: %s",
			r7DeniedScreen, status, ctype, firstLineOf(html))
	}
	if !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Fatalf("a navigating caller was answered %s, so no page was rendered and the language means nothing: %s",
			ctype, firstLineOf(html))
	}
	if contentLang != "pt-PT" {
		t.Errorf("the refusal a signed-in person is shown at %s (which declared Portuguese alone) carries "+
			"Content-Language %q: the language of a refusal is the tenant's declaration, and this one reaches "+
			"the renderer through the branch of the cure that is supposed to find the tenant already on the "+
			"request: %s", acmeHost, contentLang, firstLineOf(html))
	}
	if m := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(html); m == nil || m[1] != "pt-PT" {
		declared := "(none)"
		if m != nil {
			declared = m[1]
		}
		t.Errorf("the refusal page at %s declares lang=%q, a language this tenant never declared: %s",
			acmeHost, declared, firstLineOf(html))
	}
	if !strings.Contains(html, "Não pode fazer isto.") {
		t.Errorf("the refusal carries none of the copy this shell ships in the tenant's language "+
			"(fault.AUTH_DENIED in pt-PT): %s", firstLineOf(html))
	}

	// Pillar contract line 3, which round 6 left in its "Unverified" list: the declaration
	// this refusal was worded by is a state change, and the record of it must be readable
	// afterwards. modules/tenant writes no audit row of its own — it publishes
	// tenant.locale_set inside the transaction that changed the two columns, and
	// modules/audit subscribes to every event (module.SubscribeAll) and records each one.
	// So the row is the trail, and it names the language and the operator who chose it.
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var declared, actor string
	eventually(t, "the language declaration reaches the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT coalesce(max(payload->>'default'), ''), coalesce(max(actor::text), '')
			   FROM audit_events WHERE name = 'tenant.locale_set'`).Scan(&declared, &actor)
		return err == nil && declared == "pt-PT" && actor != ""
	})
	t.Logf("tenant.locale_set recorded default=%s actor=%s", declared, actor)
}
