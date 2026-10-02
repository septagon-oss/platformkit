package document_test

import (
	"net/http"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/document"
)

func render(t *testing.T, n g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := n.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func chrome() document.Chrome {
	return document.Chrome{
		Brand: "PlatformKit", Assets: "/admin/assets", Stylesheet: ui.Compose(design.Default()),
		Scripts: []string{"htmx.min.js", "theme.js"}, SignIn: "/admin/login",
		Attrs: map[string]string{"data-grain": "pke-grain"},
	}
}

func TestDocumentCarriesChromeRequestAndView(t *testing.T) {
	t.Parallel()
	c := chrome()
	r := document.Request{Tenant: "Acme", Inline: []g.Node{h.Script(g.Attr("nonce", "n1"), g.Raw("1"))}}
	v := document.View{Title: "Tasks", Head: []g.Node{h.Link(h.Rel("stylesheet"), h.Href("/x.css"))}}
	out := render(t, document.Document(c, r, v, h.Main(g.Text("body"))))
	for _, want := range []string{
		`<html lang="en" data-signin="/admin/login" data-grain="pke-grain" dir="ltr">`,
		`<title>Tasks · Acme</title>`,
		`href="/admin/assets/app.css?v=` + c.Stylesheet.Fingerprint + `"`,
		`<script nonce="n1">1</script>`,
		`<script src="/admin/assets/js/htmx.min.js" defer></script><script src="/admin/assets/js/theme.js" defer></script>`,
		`href="/x.css"`,
		`<body><main>body</main></body>`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("document lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "data-theme") {
		t.Fatal("a chrome that pins no theme wrote data-theme")
	}
}

func TestDocumentPinsATheme(t *testing.T) {
	t.Parallel()
	c := chrome()
	c.Theme, c.SignIn = "dark", ""
	out := render(t, document.Document(c, document.Request{}, document.View{Title: "Shop"}, h.Main()))
	if !strings.Contains(out, `<html lang="en" data-theme="dark" data-grain="pke-grain" dir="ltr">`) {
		t.Fatalf("html element is wrong:\n%s", out)
	}
	if !strings.Contains(out, `<title>Shop · PlatformKit</title>`) {
		t.Fatal("the brand is not the fallback title")
	}
	if strings.Contains(out, "data-signin") {
		t.Fatal("a chrome with no sign-in wrote data-signin")
	}
}

func TestFaultKeepsTheStatusAndTheWayBack(t *testing.T) {
	t.Parallel()
	v := document.Fault(http.StatusNotFound, "Not Found", "no such task", "/admin", "Back to the dashboard")
	if v.Status != http.StatusNotFound || v.Title != "Not Found" {
		t.Fatalf("view is %+v", v)
	}
	out := render(t, g.Group(v.Body))
	for _, want := range []string{"no such task", `href="/admin"`, "Back to the dashboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("fault lacks %q", want)
		}
	}
	if !strings.Contains(render(t, g.Group(document.Fault(422, "Unprocessable Entity", "", "/", "Home").Body)), "That did not work.") {
		t.Fatal("an empty detail has no fallback sentence")
	}
}

func TestBareIsANarrowColumn(t *testing.T) {
	t.Parallel()
	out := render(t, document.Bare([]g.Node{g.Text("card")}))
	if !strings.Contains(out, "card") || !strings.Contains(out, "max-w-sm") {
		t.Fatalf("bare frame:\n%s", out)
	}
}

func TestAViewPinsItsOwnThemeAndDropsTheThemeScript(t *testing.T) {
	c := chrome()
	r := document.Request{Inline: []g.Node{h.Script(g.Raw("stored theme"))}}
	out := render(t, document.Document(c, r, document.View{Title: "Home", Theme: "dark"}, h.Main()))
	if !strings.Contains(out, `data-theme="dark"`) {
		t.Fatal("the view's theme was not pinned on the document")
	}
	if strings.Contains(out, "stored theme") {
		t.Fatal("a pinned document carried the visitor's theme script")
	}
	out = render(t, document.Document(c, r, document.View{Title: "Home"}, h.Main()))
	if !strings.Contains(out, "stored theme") {
		t.Fatal("an unpinned document dropped the theme script")
	}
}

// TestDirectionFollowsTheLanguageAndNotTheTenant: <html dir> is the writing
// direction of the language a document is declared in — never of the tenant, the
// client or the deployment.
func TestDirectionFollowsTheLanguageAndNotTheTenant(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ tag, want string }{
		{"en", "ltr"}, {"pt-PT", "ltr"}, {"", "ltr"}, {"sr-Cyrl", "ltr"}, {"kk-Cyrl-KZ", "ltr"},
		{"ar", "rtl"}, {"ar-MA", "rtl"}, {"he-IL", "rtl"}, {"fa-AF", "rtl"}, {"ur-PK", "rtl"},
		{"pa-Arab", "rtl"}, {"pa-Guru", "ltr"},
	} {
		if got := document.Direction(c.tag); got != c.want {
			t.Errorf("Direction(%q) = %q, want %q", c.tag, got, c.want)
		}
	}
	c := chrome()
	out := render(t, document.Document(c, document.Request{}, document.View{Title: "Shop", Language: "ar"}, h.Main()))
	root := out
	if end := strings.Index(root, ">"); end >= 0 {
		root = root[:end]
	}
	if !strings.Contains(root, `lang="ar"`) || !strings.Contains(root, `dir="rtl"`) {
		t.Fatalf("an Arabic document is not declared right to left:\n%s", out)
	}
}
