package auth_test

import (
	"net/url"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestNewRefusesAnEncodedPageUnderTheAPIRoot(t *testing.T) {
	for _, page := range []string{
		"/account/%2e%2e/api/v1/public/auth/verify-email",
		"/account/.%2e/api/v1/auth/password/reset",
		"/%2e/api/v1/auth/password/reset",
		"/api/%2e/v1/public/auth/verify-email",
	} {
		u, err := url.Parse(page)
		if err != nil || !httpx.UnderAPI(u.Path) {
			t.Fatalf("fixture %q does not resolve under the API root: %v", page, err)
		}
		for name, pages := range map[string]auth.LinkPages{"set_password": {SetPassword: page}, "confirm_email": {ConfirmEmail: page}} {
			t.Run(name+page, func(t *testing.T) {
				if said := refusal(auth.Deps{LinkPages: pages}); said == "" {
					t.Errorf("New accepted %+v, whose URL path %q is under the API root", pages, u.Path)
				}
			})
		}
	}
}

func TestCompositionRefusesAnEncodedPageUnderTheAPIRoot(t *testing.T) {
	deployment := pkit.Deployment{Environment: pkit.Development}
	valid := pkit.NewApp("links").Use(auth.MailedLinksOpen(auth.LinkPages{SetPassword: "/account/password"}))
	if err := valid.Validate(deployment); err != nil {
		t.Fatalf("valid page composition: %v", err)
	}
	for _, pages := range []auth.LinkPages{
		{SetPassword: "/account/%2e%2e/api/v1/auth/password/reset"},
		{ConfirmEmail: "/account/%2e%2e/api/v1/public/auth/verify-email"},
	} {
		app := pkit.NewApp("links").Use(auth.MailedLinksOpen(pages))
		if err := app.Validate(deployment); err == nil {
			t.Errorf("composition accepted %+v, whose URL path is under the API root", pages)
		}
	}
}
