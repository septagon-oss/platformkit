package resource_test

// The two words a person reads beside one control — the label on it and the line
// under it — arrive from one request and must leave in one language. `ui/forms`
// takes both from this package, so the pair is asserted here, at the projection,
// and not only at a composition that happens to have copy for one of them.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// spoken is a hand-written locale.Formatter, which is the only kind of provider
// a unit test here can hold: xtext lives behind ui/page's Messages, and what
// this file needs to say is about a formatter's *answers*, not about a catalog
// file. It answers from a table by key, the declared fallback when the table
// lacks one, and the empty string for a key listed in `silent` — the shape of a
// foreign provider with nothing to say, which is what Words.say guards against
// (xtext itself cannot answer empty: its loader refuses a blank translation).
type spoken struct {
	words  map[string]string
	silent []string
}

func (s spoken) Text(key, fallback string, _ ...any) string {
	for _, k := range s.silent {
		if k == key {
			return ""
		}
	}
	if out, ok := s.words[key]; ok {
		return out
	}
	return fallback
}

func speak(language string, s spoken) *locale.Locale {
	return &locale.Locale{Language: language, Formatter: s}
}

// Line is the note entity this file renders: `slug` declares both a label and a
// help line, `Title` declares only a Doc. A command's argument reaches the same
// projection (controlWords), so one assertion here covers both screens.
type Line struct {
	entity.Base
	Title string `json:"title" doc:"What this line is about"`
	Slug  string `json:"slug" ui:"label:Address name;help:Used to build this address"`
}

func (Line) TableName() string { return "lines" }

func lines() resource.Resource {
	return resource.Resource{
		Schema: entity.Schema{Module: "line", Entity: "line", Path: "/api/v1/line/lines", Fields: entity.Fields[*Line]()},
		Screen: "/app/line/lines",
	}
}

func form(t *testing.T, r resource.Resource, o resource.Options) string {
	t.Helper()
	example := resource.FormExample("line-form", r, o, "/api/v1/line/lines/new", "New line", nil, nil, "", true)
	var body strings.Builder
	if err := example.Node.Render(&body); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

// TestAFormDrawsItsLabelAndItsHelpLineInOneLanguage is the pair. The label has
// always been resolved through Words.FieldLabel; the help line is what a caller
// of forms.Field used to take from display.FieldHelp, which reads no request, so
// a Portuguese form asked for a slug in Portuguese and explained it in English.
func TestAFormDrawsItsLabelAndItsHelpLineInOneLanguage(t *testing.T) {
	options := resource.Options{Locale: speak("pt-PT", spoken{words: map[string]string{
		"hints.line/line.field.slug.label": "Nome do endereço",
		"hints.line/line.field.slug.help":  "Usado para montar este endereço",
	}})}
	body := form(t, lines(), options)
	for _, want := range []string{"Nome do endereço", "Usado para montar este endereço"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Portuguese form lacks %q:\n%s", want, body)
		}
	}
	for _, english := range []string{"Address name", "Used to build this address"} {
		if strings.Contains(body, english) {
			t.Errorf("the Portuguese form carries the declared English %q beside its Portuguese words", english)
		}
	}
	// A field that declares no `help:` keeps its Doc: a developer sentence is not
	// a hint string until its author writes one, and no copy table holds it.
	if !strings.Contains(body, "What this line is about") {
		t.Errorf("the form lost the Doc of a field that declares no help:\n%s", body)
	}
}

// TestAFormatterThatAnswersNothingLeavesTheDeclaredWord is the guard in
// Words.say, written against the provider the guard exists for: one that has no
// copy and answers empty. A control whose label or help resolves to nothing is a
// control nobody may answer, so the declared sentence has to survive.
func TestAFormatterThatAnswersNothingLeavesTheDeclaredWord(t *testing.T) {
	options := resource.Options{Locale: speak("pt-PT", spoken{
		words:  map[string]string{"hints.line/line.field.slug.label": "Nome do endereço"},
		silent: []string{"hints.line/line.field.slug.help"},
	})}
	body := form(t, lines(), options)
	if !strings.Contains(body, "Used to build this address") {
		t.Errorf("an empty answer erased the declared help line:\n%s", body)
	}
	if strings.Contains(body, `aria-describedby`) == false {
		t.Error("the help line stopped being attached to its control")
	}
}
