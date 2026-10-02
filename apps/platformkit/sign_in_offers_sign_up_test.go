package main

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestTheSignInPageOffersSignUpWhenTheCompositionOptsIn is the brief's second
// decided rule's other half: the sign-in page "shows sign-up only when the
// composition opts in". This composition opts in — apps/platformkit/modules.go
// hands auth.Deps an EmailRegistration, and /api/v1/public/auth/register answers
// — so the card has to link a page that renders the registration form
// ui/assets/js/session.js already posts (`data-auth-form="register-password"`).
func TestTheSignInPageOffersSignUpWhenTheCompositionOptsIn(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})

	code, _, page := signInPageTo(t, cfg, acmeHost)
	if code != http.StatusOK {
		t.Fatalf("GET the sign-in page = %d: %s", code, firstLineOf(page))
	}
	form := regexp.MustCompile(`data-auth-form="register(-password)?"`)
	for _, m := range regexp.MustCompile(`<a[^>]*href="(/[^"]*)"`).FindAllStringSubmatch(page, -1) {
		code, body := do(t, cfg, nil, http.MethodGet, acmeHost, m[1], "")
		if code == http.StatusOK && form.MatchString(body) {
			return
		}
	}
	t.Errorf("the composition wires email registration and the sign-in page links no page that signs a person up:\n%s",
		firstLineOf(page))
}
