package main

// A reviewer's case, T-0111 review round 9 (the last round, HIGH-only remit) — item 13's
// fragile seam, pinned rather than asserted away.
//
// The sign-in page is translated: its title, its description, its two labels and its
// button come from `admin.login.*` and answer in the tenant's language
// (modules/admin/internal/pages.go:86-118). The message the person sees when they mistype
// is not server-rendered at all: `ui/assets/js/session.js:45` takes the `detail` of the
// problem the auth route answered with and writes it into the element this page marks
// `data-login-error` — and that element carries a static `lang="en"`.
//
// So one equality holds the page together, and nothing anywhere tested it: the language
// the element declares must be the language the refusal it will hold is written in. Today
// both sides say `en` — the auth route answers an API refusal in the source language of
// this repository's copy and names no `Content-Language` (measured below) — which is why
// the e2e journey's `expect(page.locator('[data-login-error]')).toHaveAttribute('lang','en')`
// is true and honest. The day a round words the auth refusal in the tenant's language, as
// five rounds of this task did the page refusals, this page would be telling a screen
// reader to read Portuguese with an English voice — the exact fault `a664013` ("a page
// whose copy is written here says so, in its own language") removed from the dashboard,
// arriving back through the one element nobody looked at.
//
// The reachability discipline of this round's instructions is honoured: every leg is
// reached through what correct behaviour prints — the page's own `lang="pt-PT"`, its
// `Content-Language: pt-PT`, the 401 and its `detail` — never through the output of a
// broken build. Nothing here asserts an English sentence as a requirement; it asserts that
// two answers about one language agree, whichever languages they are.

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// sourceLanguage is the language this repository's own copy is written in, which is what
// a problem document that names no `Content-Language` is answered in. modules/web says the
// same thing about its own Go strings (internal/mount.go: `sourceLanguage = "en"`).
const sourceLanguage = "en"

func TestTheSignInRefusalIsDeclaredInTheLanguageItIsWrittenIn(t *testing.T) {
	path, cfg := configure(t)
	install(t, path) // acme, bootstrapped --language pt-PT: it serves en and pt-PT
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	operator := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := tenantID(t, cfg, operator, "acme")
	if code, body := do(t, cfg, operator, http.MethodPost, acmeHost,
		tenantPath+"/"+id+"/locale", `{"default":"pt-PT","supported":["en"]}`); code != http.StatusOK {
		t.Fatalf("declaring the tenant's languages = %d %s", code, short(body))
	}

	// The page, asked for by a Portuguese browser. Everything below reads this response.
	code, header, page := signInPageTo(t, cfg, acmeHost)
	if code != http.StatusOK {
		t.Fatalf("GET the sign-in page = %d: %s", code, firstLineOf(page))
	}
	pageLanguage := attribute(page, `(?s)<html[^>]*\blang="([^"]*)"`)
	declared := header.Get("Content-Language")
	if pageLanguage != "pt-PT" || declared != "pt-PT" {
		t.Fatalf("a Portuguese browser was answered in html lang=%q, Content-Language=%q: the "+
			"case below cannot say anything about a page that is not the tenant's own language",
			pageLanguage, declared)
	}
	holder := attribute(page, `(?s)<[^>]*\bdata-login-error[^>]*\blang="([^"]*)"`)
	if holder == "" {
		t.Fatalf("the element the sign-in script writes its refusal into declares no language at "+
			"all, so it inherits the page's: %s", firstLineOf(page))
	}

	// The refusal the script will put in that element: the same credentials route the form
	// posts to, in the same language, wrong.
	code, refused, body := post(t, cfg, acmeHost, `{"email":"`+adminEmail+`","password":"not-the-password"}`)
	if code == http.StatusOK {
		t.Fatalf("the wrong credentials signed somebody in: %s", short(body))
	}
	detail := detailOf(t, body)
	written := detailLanguage(refused)

	if holder != written {
		t.Errorf("the element that will hold the refusal declares lang=%q while the refusal it will "+
			"hold is written in %q (%s): a screen reader is told to read one language with another's "+
			"voice. Either the element's lang follows the answer, or the answer stays in the language "+
			"the element names.", holder, written, detail)
	}
}

// signInPageTo reads the sign-in page as a navigation in Portuguese.
func signInPageTo(t *testing.T, cfg config.Config, host string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		"http://"+cfg.Server.Addr+"/app/admin/login", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	return sent(t, req)
}

// detailOf is the `detail` the browser script announces into the page.
func detailOf(t *testing.T, body string) string {
	t.Helper()
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &problem); err != nil {
		t.Fatalf("the sign-in refusal was not a problem document: %s", firstLineOf(body))
	}
	if strings.TrimSpace(problem.Detail) == "" {
		t.Fatalf("the sign-in refusal carried no detail for the page to show: %s", firstLineOf(body))
	}
	return problem.Detail
}

func post(t *testing.T, cfg config.Config, host, body string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/api/v1/auth/login", strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Content-Type", "application/json")
	return sent(t, req)
}

func sent(t *testing.T, req *http.Request) (int, http.Header, string) {
	t.Helper()
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", req.URL.Path, err)
	}
	return res.StatusCode, res.Header.Clone(), string(body)
}

// detailLanguage is the language a problem document answers in: the one it names, or the
// source language of the copy this repository is written in.
func detailLanguage(h http.Header) string {
	if got := h.Get("Content-Language"); got != "" {
		return got
	}
	return sourceLanguage
}

func attribute(body, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}
