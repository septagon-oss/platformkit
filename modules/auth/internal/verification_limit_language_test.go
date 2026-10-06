package internal_test

import (
	"net/http"
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

func TestVerificationAttemptLimitSpeaksTheRequestedLanguage(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, false, emailSignup, func(deps *auth.Deps) {
		deps.Pages.Messages = xtext.Load("en", page.Catalogue(), auth.Catalogue())
	})
	edit := func(r *http.Request) {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "text/html")
		r.Header.Set("Accept-Language", "pt-PT")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	for range contracts.ResetRedemptions {
		res := call(t, router, http.MethodPost, contracts.VerifyEmailPath, "token=invalid", edit)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("attempt inside the budget = %d, want 401", res.Code)
		}
	}
	res := call(t, router, http.MethodPost, contracts.VerifyEmailPath, "token=invalid", edit)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("exhausted verification budget = %d, want 429", res.Code)
	}
	if !strings.HasPrefix(res.Header().Get("Content-Type"), "text/html") {
		t.Fatal("limited browser did not receive its refusal page")
	}
	if got := res.Header().Get("Content-Language"); got != "pt-PT" {
		t.Errorf("verification limit Content-Language = %q, want pt-PT", got)
	}
	if !strings.Contains(res.Body.String(), `lang="pt-PT"`) ||
		strings.Contains(res.Body.String(), "too many account link attempts; wait and try again") {
		t.Error("Portuguese reader receives the English verification-limit refusal")
	}
	var accounts, tokens, emitted int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT (SELECT count(*) FROM users), (SELECT count(*) FROM verification_tokens), (SELECT count(*) FROM platformkit_outbox)").
		Scan(&accounts, &tokens, &emitted); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 || tokens != 0 || emitted != 0 {
		t.Errorf("refused verification wrote accounts=%d tokens=%d events=%d", accounts, tokens, emitted)
	}
}
