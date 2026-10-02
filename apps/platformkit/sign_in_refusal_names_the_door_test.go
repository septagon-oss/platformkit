package main

import (
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestTheSignInRefusalNamesALinkThePageShows is the brief's second decided rule in
// the tenant's own language: an unknown address "gets a sentence saying what to do
// next". The next step the refusal names is the sign-in card's forgotten-password
// link, by its label, so the label it quotes has to be one the person can find on
// the page they are reading — whichever language that page is in.
func TestTheSignInRefusalNamesALinkThePageShows(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	operator := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := tenantID(t, cfg, operator, "acme")
	if code, body := do(t, cfg, operator, http.MethodPost, acmeHost,
		tenantPath+"/"+id+"/locale", `{"default":"pt-PT","supported":["en"]}`); code != http.StatusOK {
		t.Fatalf("declaring the tenant's languages = %d %s", code, short(body))
	}

	code, _, page := signInPageTo(t, cfg, acmeHost)
	if code != http.StatusOK {
		t.Fatalf("GET the sign-in page = %d: %s", code, firstLineOf(page))
	}
	link := regexp.MustCompile(`<a[^>]*href="[^"]*/login/forgot"[^>]*>([^<]+)</a>`).FindStringSubmatch(page)
	if link == nil {
		t.Fatalf("the sign-in page offers no forgotten-password link: %s", firstLineOf(page))
	}
	label := html.UnescapeString(strings.TrimSpace(link[1]))

	code, body := do(t, cfg, nil, http.MethodPost, acmeHost, "/api/v1/auth/login",
		`{"email":"nobody@acme.localhost","password":"not anybody's password"}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("an unknown address = %d %s, want 401", code, short(body))
	}
	var problem struct {
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &problem); err != nil {
		t.Fatalf("read the refusal: %v (%s)", err, short(body))
	}
	// Every label the refusal quotes is one the page shows; a refusal that quotes
	// none and still says what to do next is as good an answer.
	for _, quoted := range regexp.MustCompile(`["“«]([^"”»]+)["”»]`).FindAllStringSubmatch(problem.Detail, -1) {
		if quoted[1] != label && !strings.Contains(page, quoted[1]) {
			t.Errorf("the refusal sends the person to %q, which the page they are reading does not show (its link reads %q):\n  %s",
				quoted[1], label, problem.Detail)
		}
	}
}
