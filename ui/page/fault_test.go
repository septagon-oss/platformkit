package page_test

// The kernel decides *when* a refusal becomes a page. These tests are about what the
// page is, because that is where the damage lands: a refusal page that leaks what broke,
// or answers 200 while saying Forbidden, or has no way out of it, is worse than the JSON
// it replaced — the JSON at least never pretended to be something it was not.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/page"
)

func shell() page.Shell {
	return page.Shell{
		Chrome: page.Chrome{Brand: "Demo", SignIn: "/admin/login", Stylesheet: ui.Compose(design.Default()), Assets: "/admin/assets"},
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
			return h.Div(h.Class("pkit-shell"), g.Group(body))
		},
		Back:      "/admin",
		BackLabel: "Back to the workspace",
	}
}

func renderFault(t *testing.T, s page.Shell, p *problem.Problem, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	fault := page.FaultHandler(s)
	if fault == nil {
		t.Fatal("no fault renderer")
	}
	w := httptest.NewRecorder()
	if !fault(w, r, p) {
		t.Fatal("the renderer declined a shell that is fully configured")
	}
	return w
}

// TestARefusalPageCarriesItsOwnVerdictAndAWayOut. The status is the verdict's, the title
// says what happened, and one link leaves — a dead end is what a person in front of an
// error page cannot recover from.
func TestARefusalPageCarriesItsOwnVerdictAndAWayOut(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://demo.localhost/admin/tasks", nil)

	got := renderFault(t, shell(), &problem.Problem{
		Status:   http.StatusForbidden,
		Detail:   "csrf: this request carries a session cookie and came from another site",
		Instance: "urn:request:1111aaaa-2222-3333-4444-555566667777",
	}, req)

	if got.Code != http.StatusForbidden {
		t.Errorf("the page answered %d while telling the person it was forbidden", got.Code)
	}
	body := got.Body.String()
	for _, want := range []string{
		"Forbidden", // the verdict in words, for someone who cannot see the status
		"csrf:",     // the curated reason, which is actionable here
		"Back to the workspace",
		`href="/admin"`,
		"1111aaaa-2222-3333-4444-555566667777", // the reference an operator can search a log for
		`href="/admin/assets/app.css`,          // the shell's own stylesheet: it looks like the application
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the refusal page omits %q\n%s", want, trim(body))
		}
	}
}

// TestARefusalPageCannotBeMadeToSpeak detail is written by the kernel, but the page must
// treat it as data: a 500's reason, a path, an SQL fragment or an attacker-influenced
// string must not become markup. This is the one property that would be a vulnerability
// rather than a defect.
func TestARefusalPageCannotBeMadeToSpeak(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/admin", nil)

	got := renderFault(t, shell(), &problem.Problem{
		Status: http.StatusBadRequest,
		Detail: `<script>alert("xss")</script> & "quoted" <b>bold</b>`,
	}, req)

	body := got.Body.String()
	if strings.Contains(body, "<script>alert") {
		t.Errorf("detail became markup; it is not ours to interpret: %s", trim(body))
	}
	for _, want := range []string{"&lt;script&gt;", "&amp;", "&lt;b&gt;bold&lt;/b&gt;"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s, the escaped form, in the page\n%s", want, trim(body))
		}
	}
}

// TestAFiveHundredSaysSomethingTrueWithoutSayingWhatBroke. The kernel hands a 500 with no
// detail on purpose; the page still has to be worth reading, and still has to give the
// reference, because a 500 is precisely the failure the person cannot resolve themselves.
func TestAFiveHundredSaysSomethingTrueWithoutSayingWhatBroke(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/admin", nil)

	got := renderFault(t, shell(), &problem.Problem{
		Status:   http.StatusInternalServerError,
		Instance: "urn:request:deadbeef-0000-1111-2222-333344445555",
	}, req)

	if got.Code != http.StatusInternalServerError {
		t.Errorf("a 500 page answered %d", got.Code)
	}
	body := got.Body.String()
	if !strings.Contains(body, "deadbeef-0000-1111-2222-333344445555") {
		t.Errorf("the 500 page gives no reference, so the person cannot report it: %s", trim(body))
	}
	if strings.Contains(body, "Internal Server Error") == false {
		t.Errorf("the 500 page never says what it is: %s", trim(body))
	}
}

// TestAShellWithNoFrameDeclinesRatherThanInventingAPage: false means "fall back to the
// problem document", which is honest. A half-drawn page suggests the application is
// broken rather than that the request was refused.
func TestAShellWithNoFrameDeclinesRatherThanInventingAPage(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://demo.localhost/admin", nil)
	fault := page.FaultHandler(page.Shell{Chrome: page.Chrome{Brand: "Demo", Stylesheet: ui.Compose(design.Default())}})

	w := httptest.NewRecorder()
	if fault(w, req, &problem.Problem{Status: http.StatusForbidden, Detail: "no"}) {
		t.Error("a shell with no frame claimed to have rendered a page")
	}
	if w.Body.Len() != 0 {
		t.Errorf("a declining renderer wrote %d bytes anyway, which the kernel will follow with its own body", w.Body.Len())
	}
}

func trim(body string) string {
	const keep = 900
	if len(body) > keep {
		return body[:keep] + "…"
	}
	return body
}
