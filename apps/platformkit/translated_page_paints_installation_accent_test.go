package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

// TestATranslatedPageIsPaintedInTheInstallationsAccent reads the public page
// through the door the translation module added to the same shell: a reader who
// asks for Portuguese of a page the tenant translated. The accent a page is drawn
// in is a fact about the tenant's settings and not about the language asked for,
// so a tenant that named no accent is painted in the installation's own pair on
// the Portuguese document exactly as on the English one. What a tenant chose is
// stored, not painted: each mode draws the colour its own canvas clears.
func TestATranslatedPageIsPaintedInTheInstallationsAccent(t *testing.T) {
	cfg, admin, _ := publishedTranslationPage(t, map[string]string{"title": "A nossa história", "body": "O parágrafo português."})
	pair := design.Default()
	installation := []string{
		":root{--pk-color-accent-default:" + pair.Light.AccentDefault + "}",
		`[data-theme="dark"]{--pk-color-accent-default:` + pair.Dark.AccentDefault + "}",
		"@media (prefers-color-scheme: dark){:root:not([data-theme]){--pk-color-accent-default:" + pair.Dark.AccentDefault + "}}",
	}

	for _, page := range []struct{ path, accept, language, copy string }{
		{"/translated-story?lang=pt-PT", "pt-PT", "pt-PT", "A nossa história"},
		{"/translated-story", "en", "en", "Our story"},
	} {
		status, html := getLanguage(t, cfg, acmeHost, page.path, page.accept)
		if status != http.StatusOK || !strings.Contains(html, `<html lang="`+page.language+`"`) || !strings.Contains(html, page.copy) {
			t.Fatalf("GET %s = %d, want 200 in %s with %q", page.path, status, page.language, page.copy)
		}
		for _, want := range installation {
			if !strings.Contains(html, want) {
				t.Errorf("GET %s of a tenant that named no accent is missing %q", page.path, want)
			}
		}
		if strings.Contains(html, "#2563eb") {
			t.Errorf("GET %s carries the withdrawn default colour", page.path)
		}
	}

	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, sitePath,
		`{"title":"Acme Works","primaryColor":"#b45309"}`); code != http.StatusOK {
		t.Fatalf("choosing a colour = %d %s", code, body)
	}
	status, html := getLanguage(t, cfg, acmeHost, "/translated-story?lang=pt-PT", "pt-PT")
	if status != http.StatusOK || !strings.Contains(html, `<html lang="pt-PT"`) {
		t.Fatalf("GET the Portuguese page after choosing a colour = %d, want 200 in pt-PT", status)
	}
	// The colour a tenant chose is measured where it is painted. #b45309 reads
	// 4.37:1 on the light canvas and 3.66:1 on the dark one, so the write door
	// admits it — its floor is 3.0 against both canvases, because a row is edited
	// once and read in either — and the paint asks the 4.5 of the theme it is
	// drawing, taking the nearest colour along the same hue when the stored one
	// falls short (design.Pair.LegibleAccent). Pinning #b45309 over both modes would
	// be painting a colour the stylesheet's own gate refuses, on the page that links
	// that stylesheet.
	if !strings.Contains(html, ":root{--pk-color-accent-default:#9c531c}") {
		t.Error("the Portuguese page of a tenant that chose #b45309 is not painted in the colour the gate issues for it on the light canvas")
	}
	if !strings.Contains(html, `[data-theme="dark"]{--pk-color-accent-default:#ca7e43}`) {
		t.Error("the dark mode of a page painted in a tenant's chosen colour carries no colour measured for the dark canvas")
	}
}
