package page

// A withheld reason is a verdict with no sentence under it, and the page still owes the
// reader one — in their language.
//
// Two cases meet it. A 500 has carried no Detail since this package began, and a
// problem.Diagnostic carries one the writer kept for the log; a page built from either has
// nothing of the writer's to show. What was shown before this file was the 500's own line,
// "Something went wrong while handling this.", whatever the verdict — so a person refused
// because they had not signed in was told their own verdict was our broken end, in English,
// under a document declaring Portuguese. `faultKey` now keys that case by its verdict for
// every status whose sentence this package writes, and this file is the gate that the lines
// it keys are the lines actually on the page, in the language the request asked for.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

const withheldDiagnostic = "database-primary.internal SELECT secret FROM credentials"

// withheldVerdicts is every status the shell can be asked to refuse with its reason kept
// back, read out of faultKey's own behaviour rather than listed from memory: ask it with no
// sentence at all and take whatever it keys. A status added to that switch arrives here with
// its sentence, or this file says so — which is the same gate review_r3_verdict_copy_test.go
// holds on the catalogue, run against the rendered page instead of the file.
func withheldVerdicts(t *testing.T) []int {
	t.Helper()
	var statuses []int
	for code := 400; code < 600; code++ {
		if key, lookup := faultKey("", code); lookup && key == "fault."+strconv.Itoa(code) {
			statuses = append(statuses, code)
		}
	}
	if len(statuses) == 0 {
		t.Fatal("no withheld verdict is keyed by its status, which means this gate stopped looking")
	}
	return statuses
}

func translatedShell() Shell {
	return Shell{
		Chrome: Chrome{Brand: "Demo", SignIn: "/admin/login", Stylesheet: ui.Compose(design.Default()), Assets: "/admin/assets"},
		Frame: func(_ context.Context, _ Request, body []g.Node) g.Node {
			return h.Div(h.Class("pkit-shell"), g.Group(body))
		},
		Back:     "/admin",
		Messages: xtext.Load("en", Catalogue()),
	}
}

// TestAWithheldReasonSpeaksTheRequestedLanguage. The diagnostic stays out, the 500's line
// stays off a 4xx, and the sentence that replaces both is the catalogue's.
func TestAWithheldReasonSpeaksTheRequestedLanguage(t *testing.T) {
	t.Parallel()
	portuguese := shippedCopy(t, "pt-PT")
	for _, status := range withheldVerdicts(t) {
		key := "fault." + strconv.Itoa(status)
		sentence, carried := portuguese[key]
		if !carried || strings.TrimSpace(sentence) == "" {
			t.Fatalf("%q carries no Portuguese sentence, and a withheld %d is keyed by its verdict", key, status)
		}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "http://demo.localhost/admin/tasks", nil)
		req.Header.Set("Accept-Language", "pt-PT")
		refused := problem.New(status, withheldDiagnostic)
		refused.Diagnostic = true
		rec := httptest.NewRecorder()
		if !FaultHandler(translatedShell())(rec, req, refused) {
			t.Fatalf("the renderer declined a configured shell at %d", status)
		}
		if rec.Code != status {
			t.Errorf("a withheld %d answered %d", status, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, withheldDiagnostic) {
			t.Errorf("a withheld %d page repeats the operator's diagnostic", status)
		}
		if !strings.Contains(body, sentence) {
			t.Errorf("a withheld %d page does not say %q, the sentence the catalogue ships for it: %s",
				status, sentence, clipped(body))
		}
		if strings.Contains(body, "Something went wrong while handling this.") {
			t.Errorf("a withheld %d page blames our end for the reader's own verdict: %s", status, clipped(body))
		}
		if got := rec.Header().Get("Content-Language"); got != "pt-PT" {
			t.Errorf("a withheld %d page declared %q to a Portuguese reader", status, got)
		}
		if !strings.Contains(body, `lang="pt-PT"`) {
			t.Errorf("a withheld %d page declares no Portuguese to the software reading it aloud", status)
		}
	}
}

// TestAWithheldReasonKeepsTheWaitAndTheWayOut. Withholding the reason may not withhold the
// two things a person acts on: the reference an operator can grep a log for, and the way out
// of a page with nothing else on it.
func TestAWithheldReasonKeepsTheWaitAndTheWayOut(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://demo.localhost/admin", nil)
	req.Header.Set("Accept-Language", "pt-PT")
	refused := problem.New(http.StatusServiceUnavailable, withheldDiagnostic)
	refused.Diagnostic = true
	refused.Instance = "urn:request:cc0a0000-1111-2222-3333-444455556666"
	rec := httptest.NewRecorder()
	if !FaultHandler(translatedShell())(rec, req, refused) {
		t.Fatal("the renderer declined a configured shell")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "cc0a0000-1111-2222-3333-444455556666") {
		t.Errorf("the withheld reason took the reference with it: %s", clipped(body))
	}
	if strings.Contains(body, withheldDiagnostic) {
		t.Errorf("the withheld 503 page repeats the operator's diagnostic: %s", clipped(body))
	}
	if !strings.Contains(body, `href="/admin"`) {
		t.Errorf("the withheld 503 page lost its way out: %s", clipped(body))
	}
}

// shippedCopy reads the deployment's own file, so the assertion is on the shipped sentence
// rather than on a string this file repeats.
func shippedCopy(t *testing.T, language string) map[string]string {
	t.Helper()
	body, err := catalogues.ReadFile("messages/" + language + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var shipped map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(body, &shipped); err != nil {
		t.Fatalf("messages/%s.json is not gotext JSON: %v", language, err)
	}
	out := map[string]string{}
	for key, message := range shipped {
		out[key] = message.Translation
	}
	return out
}

// clipped is enough of a page to read in a failure message.
func clipped(body string) string {
	if len(body) > 1200 {
		return body[:1200]
	}
	return body
}
