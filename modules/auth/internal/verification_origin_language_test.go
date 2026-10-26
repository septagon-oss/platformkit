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

func TestCrossSiteVerificationSpeaksTheRequestedLanguageAndKeepsTheCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(deps *auth.Deps) {
		deps.Pages.Messages = xtext.Load("en", page.Catalogue(), auth.Catalogue())
	})
	_, token := verificationSignup(t, conn, router, "origin-recipient@example.com")
	snapshot := func() string {
		t.Helper()
		var rows string
		if err := admin.QueryRowContext(t.Context(), `SELECT jsonb_build_object(
			'users', (SELECT jsonb_agg(u ORDER BY id) FROM users u),
			'tokens', (SELECT jsonb_agg(v ORDER BY token_hash) FROM verification_tokens v),
			'events', (SELECT jsonb_agg(e ORDER BY id) FROM platformkit_outbox e))::text`).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := snapshot()
	for _, language := range []string{"en", "pt-PT"} {
		t.Run(language, func(t *testing.T) {
			res := call(t, router, http.MethodPost, contracts.VerifyEmailPath,
				"token="+url.QueryEscape(token), func(r *http.Request) {
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					r.Header.Set("Accept", "text/html")
					r.Header.Set("Accept-Language", language)
					r.Header.Set("Sec-Fetch-Site", "cross-site")
				})
			if res.Code != http.StatusForbidden {
				t.Fatalf("cross-site verification = %d, want 403", res.Code)
			}
			if got := res.Header().Get("Content-Language"); got != language {
				t.Errorf("refusal language = %q, want %q", got, language)
			}
			want := "confirm the email from the verification page itself"
			if language == "pt-PT" {
				want = "Confirme o e-mail a partir da própria página de confirmação."
			}
			if !strings.Contains(res.Body.String(), want) || !strings.Contains(res.Body.String(), `lang="`+language+`"`) {
				t.Errorf("refusal does not speak %s", language)
			}
			if strings.Contains(res.Body.String(), token) || strings.Contains(res.Body.String(), "origin-recipient@example.com") {
				t.Error("cross-site refusal exposes the credential or its recipient")
			}
			if got := snapshot(); got != before {
				t.Error("cross-site refusal changed an account, credential or outbox row")
			}
		})
	}
	res := call(t, router, http.MethodPost, contracts.VerifyEmailPath, "token="+url.QueryEscape(token), func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	})
	if res.Code != http.StatusSeeOther {
		t.Errorf("same-origin confirmation after refusals = %d, want 303", res.Code)
	}
}
