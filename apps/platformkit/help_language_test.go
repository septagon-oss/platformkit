package main

// A declared `help:` is a hint string like any other: the brief's language rule
// ("every hint string resolves for the request's Accept-Language") and the
// specification's second resolution site both name the line under a control. The
// catalogue document already serves it translated, so a web form that draws it in
// English answers one person's request in two languages on one screen.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// TestADeclaredHelpLineIsDrawnInTheRequestLanguage renders the content create
// form in Portuguese: `slug` declares both a label and a help line, and both have
// Portuguese copy in modules/content/messages/pt-PT.json. The label is the
// reachability probe — it is what the fixed behaviour prints, and it already
// resolves — and the help line is the assertion.
func TestADeclaredHelpLineIsDrawnInTheRequestLanguage(t *testing.T) {
	content := resourceAt(t, "content", "content")
	selected := page.SelectLocale(page.Messages(catalogues()), "pt-PT")
	options := screens.Options{Locale: &selected}
	example := screens.FormExample("content-form-pt", content, options,
		"/api/v1/content/contents/new", "New page", nil, nil, "", true)
	var body strings.Builder
	if err := example.Node.Render(&body); err != nil {
		t.Fatal(err)
	}
	form := body.String()
	if !strings.Contains(form, "Nome do endereço") {
		t.Fatalf("the Portuguese form did not draw the declared label of slug; the copy table is not being read at all:\n%s", form)
	}
	if !strings.Contains(form, "Usado para montar o endereço web desta página") {
		t.Error("the Portuguese form draws the declared help of slug in English; a declared help line is a hint string and resolves for the request's language")
	}
	if strings.Contains(form, "Used to build this page's web address") {
		t.Error("the Portuguese form carries the English help line beside Portuguese labels: one request, two languages on one screen")
	}
}
