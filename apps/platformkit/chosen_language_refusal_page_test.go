package main

// The composition's half of one promise: the language a person chose is what answers
// them, and that includes the answer that is a refusal.
//
// ui/page negotiates a refusal through the shell's own preference resolver. The
// reference application hands that resolver to the shells page.Serve mounts, and this
// file asks whether the page the kernel's guards render — the one no module's shell
// mounts, because a guard refuses ahead of any handler — is built from the same shell or
// from a second one that reads less about the caller.
//
// The control is the same person's own screen: Portuguese chosen once, an English
// browser, and the page they may see is Portuguese. The case then asks that person a
// screen the member role holds nothing about, and reaches its assertion through the
// verdict (403) and the media type (a page), never through which words a wrong answer
// would print.

import (
	"net/http"
	"strings"
	"testing"

	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestAPersonsChosenLanguageAnswersTheirRefusal: the durable choice outranks
// Accept-Language on the page a guard renders, exactly as it does on the pages a module
// mounts — and the refusal is only reachable by someone who is signed in, which is the
// only reason it can be answered in a chosen language at all.
func TestAPersonsChosenLanguageAnswersTheirRefusal(t *testing.T) {
	cfg, _ := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const email = "refusal-language@acme.test"
	provisionAs(t, cfg, compose(cfg), acmeTenant(t, cfg), email, authcontracts.RoleMember)
	member := signIn(t, cfg, acmeHost, email, adminPass)

	// The choice, written through the account's own door rather than any fixture write.
	if code, body := do(t, cfg, member, http.MethodPost, acmeHost,
		"/api/v1/user/users/language", `{"locale":"pt-PT"}`); code != http.StatusOK {
		t.Fatalf("choosing pt-PT = %d: %s", code, body)
	}

	// The control, at a screen this person may see: the browser named only English, and
	// the answer is theirs. If this fails, the case below means nothing.
	if at := pageLang(dashboard(t, cfg, member, "en-GB,en;q=0.9")); at != "pt-PT" {
		t.Fatalf("the screen this person may see declares lang=%q rather than the pt-PT they chose: "+
			"the fixture's own preference never reached this shell, so the refusal below proves nothing", at)
	}

	// The refusal: same caller, same session, same header, one address over.
	status, ctype, declared, html := r7navigate(t, cfg, member, acmeHost, r7DeniedScreen, "en-GB,en;q=0.9")
	if status != http.StatusForbidden || !strings.Contains(strings.ToLower(ctype), "text/html") {
		t.Fatalf("GET %s for a member holding no grant = %d (%s), want a page at 403: %s",
			r7DeniedScreen, status, ctype, firstLineOf(html))
	}
	if declared != "pt-PT" || pageLang(html) != "pt-PT" {
		t.Errorf("the refusal answers in %q with its document declaring %q, where the screen this same "+
			"person may see answers in pt-PT: a language someone chose has to reach the page the guards "+
			"render and not only the ones a module mounts (%s)", declared, pageLang(html), firstLineOf(html))
	}
}
