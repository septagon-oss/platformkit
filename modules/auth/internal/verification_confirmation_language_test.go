package internal_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestVerificationConfirmationSpeaksTheRequestedLanguage(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(deps *auth.Deps) {
		deps.Pages.Messages = xtext.Load("en", page.Catalogue(), auth.Catalogue())
	})
	const address = "portuguese-recipient@example.com"
	_, token := verificationSignup(t, conn, router, address)
	for _, language := range []string{"en", "pt-PT"} {
		t.Run(language, func(t *testing.T) {
			res := call(t, router, http.MethodGet, contracts.VerifyEmailPath+"?token="+url.QueryEscape(token), "", func(r *http.Request) {
				r.Header.Set("Accept", "text/html")
				r.Header.Set("Accept-Language", language)
			})
			if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), address) {
				t.Fatalf("confirmation did not show its live credential's addressee: status %d", res.Code)
			}
			if got := res.Header().Get("Content-Language"); got != language {
				t.Errorf("confirmation Content-Language = %q, want %q", got, language)
			}
			if language == "pt-PT" && strings.Contains(res.Body.String(), "Confirm your email address") {
				t.Error("the Portuguese recipient is shown the hard-coded English confirmation")
			}
		})
	}
}
