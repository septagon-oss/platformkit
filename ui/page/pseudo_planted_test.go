package page_test

// The brief's acceptance case: plant one English literal on a served page and show
// that the gate names it — the page it is on and its path in the document tree.
//
// This is the half of the pseudo-locale gate no source scan can do. Half of this
// repository's Text() calls build their key at run time, so a grep cannot say what is
// translated; a rendered page can, because the pseudo-locale provider marks every
// sentence it was asked for and a literal is left unmarked. The page below is not the
// reference application's — a planted page is the honest way to prove the instrument
// finds what it is for, and apps/platformkit's gate runs the same scan over every
// document the shipped composition serves.

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/legible"
	"github.com/septagon-oss/platformkit/ui/page"
)

func TestThePseudoLocaleGateNamesAPlantedLiteralByPageAndPath(t *testing.T) {
	_, conn := dbtest.Schema(t)
	who := privacyTenant{}
	kernel, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: "localhost", Tenants: who, Conn: conn, Authorize: who,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := shell()
	// The kernel's own refusal sentences, plus the one label this page asks for.
	// `screens.edit` is the key ui/resource raises for a row's edit button.
	s.Messages = pseudo.Wrap(xtext.Load("en", page.Catalogue(), xtext.Source{
		Name: "the case",
		FS: fstest.MapFS{"pt-PT.json": &fstest.MapFile{
			Data: []byte(`{"screens.edit": {"translation": "Editar"}}`),
		}},
	}), nil)
	page.Serve(public(kernel), s,
		page.Route{ID: "planted", Method: http.MethodGet, Path: "/_planted"}, httpx.Public(),
		func(_ context.Context, r page.Request, _ *page.Empty) (page.View, error) {
			return page.View{Body: []g.Node{
				h.Div(g.Text("Hard-coded")),
				h.Div(g.Text(r.Locale.Text("screens.edit", "Edit"))),
			}}, nil
		})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/_planted", nil)
	req.Header.Set("Accept-Language", "pt-PT")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("the page answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Language"); got != pseudo.Tag {
		t.Errorf("Content-Language = %q, want the pseudo tag the copy is marked in", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "⟦Édítár⟧") {
		t.Errorf("the sentence the catalogue answers is not marked in the document it was rendered into: %s", body)
	}

	const pageName = "GET /_planted"
	collected, err := legible.Scan([]byte(body), pseudo.Wrapped)
	if err != nil {
		t.Fatalf("legible.Scan: %v", err)
	}
	// The attribution itself: the brief's sentence is that the gate names the literal
	// *with its page and its path*, and the line format is where that claim lives. The
	// page's name is the caller's, the path is the document's, and the case below
	// asserts the whole line rather than one half of it.
	unreachable, declined := legible.Report(pageName, collected)
	want := `TEXT GET /_planted html[1] > body[2] > div[1] > div[1]#text "Hard-coded"`
	if !slices.Contains(unreachable, want) {
		t.Fatalf("the report names no literal as\n%s\ngot:\n%s", want, strings.Join(unreachable, "\n"))
	}
	for _, line := range unreachable {
		if !strings.HasPrefix(line, "TEXT "+pageName+" ") {
			t.Errorf("a violation is reported outside the page that holds it: %q", line)
		}
	}
	for _, line := range declined {
		if !strings.HasPrefix(line, "DATA "+pageName+" ") {
			t.Errorf("an exempt string is reported outside the page that holds it: %q", line)
		}
	}
	var found []legible.String
	for _, s := range legible.Violations(collected) {
		if s.Text == "Hard-coded" {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the report names no literal %q among %d strings: %+v", pageName, len(collected), collected)
	}
	if want := "html[1] > body[2] > div[1] > div[1]#text"; found[0].Path != want {
		t.Errorf("the literal is at %q, want the path %q the planted div sits at", found[0].Path, want)
	}
	// Nothing the catalogue answered is a violation: the report names the literal, and
	// the marked copy is not in it. (This page sets no title, so the frame's own
	// `· Demo` is a second unmarked string, and correct to report.)
	for _, s := range legible.Violations(collected) {
		if pseudo.Wrapped(s.Text) {
			t.Errorf("the report names %q at %s, which is marked copy", s.Text, s.Path)
		}
	}
}
