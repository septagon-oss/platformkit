package internal

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/style"
)

func render(t *testing.T, nodes []g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := g.Group(nodes).Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestTheDocumentPinsOnlyAThemeAndAColourItRecognises: the tenant's colour is
// written into a style element, so the grammar here is the second line of
// defence behind the site module's — anything that is not six hex digits is
// dropped rather than interpolated, and a theme that is not light or dark
// leaves the visitor's own choice in force.
func TestTheDocumentPinsOnlyAThemeAndAColourItRecognises(t *testing.T) {
	t.Parallel()
	site := Site{}
	pinned := site.view(&sitecontracts.SiteSettings{Theme: "dark", PrimaryColor: "#c0ffee"}, page.Request{}, "Welcome", nil)
	if pinned.Theme != "dark" {
		t.Errorf("theme = %q, want dark", pinned.Theme)
	}
	if head := render(t, pinned.Head); !strings.Contains(head, ":root{--pk-color-accent-default:#c0ffee}") {
		t.Errorf("head = %s", head)
	}
	for _, colour := range []string{"red", "#fff", "#c0ffee}body{display:none", "#C0FFEE;", "", "url(x)"} {
		v := site.view(&sitecontracts.SiteSettings{Theme: "sepia", PrimaryColor: colour}, page.Request{}, "Welcome", nil)
		if v.Theme != "" || len(v.Head) != 0 {
			t.Errorf("colour %q / theme sepia was pinned: theme=%q head=%d", colour, v.Theme, len(v.Head))
		}
	}
	if v := site.view(&sitecontracts.SiteSettings{Theme: "light", PrimaryColor: "#C0FFEE"}, page.Request{}, "Welcome", nil); v.Theme != "light" || len(v.Head) != 1 {
		t.Error("upper-case hex and the light theme are valid and were refused")
	}
}

// TestTheSiteNamesItselfFromSettingsThenTenantThenBrand is the fallback chain
// the header and footer share.
func TestTheSiteNamesItselfFromSettingsThenTenantThenBrand(t *testing.T) {
	t.Parallel()
	acme := page.Request{Tenant: tenancy.Tenant{Name: "Acme"}}
	if got := name(&sitecontracts.SiteSettings{Title: "Acme Journal"}, acme); got != "Acme Journal" {
		t.Errorf("title = %q", got)
	}
	if got := name(&sitecontracts.SiteSettings{}, acme); got != "Acme" {
		t.Errorf("tenant = %q", got)
	}
	if got := name(&sitecontracts.SiteSettings{}, page.Request{}); got != brand {
		t.Errorf("brand = %q", got)
	}
	body := render(t, []g.Node{Site{}.header(&sitecontracts.SiteSettings{Tagline: "Notes", Nav: sitecontracts.Nav{{Label: "About", Path: "/about"}}}, acme)})
	for _, want := range []string{">Acme<", "Notes", `href="/about"`, `aria-label="Site navigation"`} {
		if !strings.Contains(body, want) {
			t.Errorf("header lacks %q:\n%s", want, body)
		}
	}
}

// TestTheEmptyStatesLeadToTheAdmin: a fresh site and an unpublished home both
// say what to do next and where.
func TestTheEmptyStatesLeadToTheAdmin(t *testing.T) {
	t.Parallel()
	// Where the admin is mounted is not this module's fact — modules/admin owns
	// it — so the composition hands the address over and the empty state links
	// what it was given. This is the whole of Site.SignIn.
	const admin = "/app/admin/login"
	fresh, later := render(t, Site{SignIn: admin}.nothingYet()), render(t, Site{SignIn: admin}.notPublished("welcome"))
	if !strings.Contains(fresh, "Nothing published yet") || !strings.Contains(fresh, `href="`+admin+`"`) {
		t.Errorf("fresh site:\n%s", fresh)
	}
	if !strings.Contains(later, "welcome") || !strings.Contains(later, `href="`+admin+`"`) {
		t.Errorf("unpublished home:\n%s", later)
	}
	for path, ok := range map[string]bool{"hello-world": true, "a1": true, "Not-A-Slug": false, "a--b": false, "-a": false, "": false, "favicon.ico": false} {
		if slug.MatchString(path) != ok {
			t.Errorf("slug %q accepted = %v, want %v", path, !ok, ok)
		}
	}
}

// TestTheProseSheetIsWrittenInScaleSteps: the article's lengths are the same
// steps the components use, read back from ui/style, and the colours are the
// theme's tokens so a tenant's palette reaches the body text.
func TestTheProseSheetIsWrittenInScaleSteps(t *testing.T) {
	t.Parallel()
	css := prose().CSS()
	for _, want := range []string{
		"font-size: " + style.Text3XL.Value() + ";", "font-size: " + style.Text2XL.Value() + ";",
		"margin: " + style.S8.Value() + " 0 " + style.S3.Value() + ";", "padding-left: " + style.S6.Value() + ";",
		"border-left: " + style.Border4.Value() + " solid;", "border-radius: " + style.RadiusLG.Value() + ";",
		"color: var(--pk-color-accent-default);", "background: var(--pk-color-surface-muted);",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("prose lacks %q", want)
		}
	}
	if strings.Contains(css, "2rem;\n") && !strings.Contains(css, "margin: 2rem 0") {
		t.Error("a raw 2rem length survived outside the margin shorthand")
	}
}
