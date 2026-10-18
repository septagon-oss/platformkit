package internal

import (
	"context"
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/page"
)

// TestTheForgotPagesConfirmationDeclaresTheLanguageItIsWrittenIn: the confirmation
// the forgot page shows after a request is server-rendered copy from the catalogue,
// so in a pt-PT request it is Portuguese — and the element holding it has to say so,
// or a screen reader reads Portuguese with an English voice (the fault a664013 took
// off the dashboard). Reached through the translated text itself, never through the
// attribute this case is about.
func TestTheForgotPagesConfirmationDeclaresTheLanguageItIsWrittenIn(t *testing.T) {
	t.Parallel()
	const sent = "Se este endereço puder receber emails da conta, será enviada uma ligação."
	pt := &page.Locale{Language: "pt-PT", Formatter: words{"admin.forgot.sent": sent}}
	body := render(t, g.Group(forgotPassword(context.Background(), pt, "/api/v1/auth/password/forgot", "/app/admin/login").Body))
	if !strings.Contains(body, sent) {
		t.Fatalf("the forgot page did not render the catalogue's confirmation:\n%s", body)
	}
	holder := regexp.MustCompile(`<[^>]*\bdata-auth-message\b[^>]*>`).FindString(body)
	if holder == "" {
		t.Fatalf("no element carries the confirmation:\n%s", body)
	}
	if m := regexp.MustCompile(`\blang="([^"]*)"`).FindStringSubmatch(holder); m != nil && m[1] != pt.Language {
		t.Errorf("the confirmation is written in %s and its element declares lang=%q:\n%s", pt.Language, m[1], holder)
	}
}
