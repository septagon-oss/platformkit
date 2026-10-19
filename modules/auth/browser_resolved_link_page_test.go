package auth_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBrowserResolvedAPIPagesAreRefusedAtBothCompositionDoors(t *testing.T) {
	deployment := pkit.Deployment{Environment: pkit.Development}
	for _, page := range []string{
		"/api/v1//../auth/password/reset",
		"/api//../v1/public/auth/verify-email",
		"/account/%2E%2e/api/v1/auth/password/reset",
		"/account/.%2E/api/v1/public/auth/verify-email",
		"/%61pi/v1/auth/password/reset",
		"/api%2fv1/public/auth/verify-email",
	} {
		for _, pages := range []auth.LinkPages{{SetPassword: page}, {ConfirmEmail: page}} {
			if said := refusal(auth.Deps{LinkPages: pages}); !strings.Contains(said, "under the API root") {
				t.Errorf("New(%+v) = %q, want API-page refusal", pages, said)
			}
			app := pkit.NewApp("links").Use(auth.MailedLinksOpen(pages))
			if err := app.Validate(deployment); err == nil || !strings.Contains(err.Error(), "under the API root") {
				t.Errorf("Validate(%+v) = %v, want API-page refusal", pages, err)
			}
		}
	}
	for _, page := range []string{"/api/v10/account", "/api/v1/../account", "/account/%2e/password"} {
		pages := auth.LinkPages{SetPassword: page, ConfirmEmail: page}
		if said := refusal(auth.Deps{LinkPages: pages}); said != "" {
			t.Errorf("New(%+v) refused an ordinary page: %s", pages, said)
		}
		app := pkit.NewApp("links").Use(auth.MailedLinksOpen(pages))
		if err := app.Validate(deployment); err != nil {
			t.Errorf("Validate(%+v) refused an ordinary page: %v", pages, err)
		}
	}
}
