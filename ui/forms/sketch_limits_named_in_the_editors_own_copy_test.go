package forms_test

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// sketchCeilings pairs each ceiling the drawing value declares with what the editor hands a
// person about it: the attribute the browser enforces, and the attribute carrying the sentence
// a person is shown when the drawing breaks it. The enforced attribute is written from the
// constant; each sentence states the same figure a second time, as prose. A prose copy that has
// stopped naming the enforced bound tells a person to undo a stroke the field would accept, or
// to keep one the server refuses on save.
var sketchCeilings = []struct {
	value   int64
	limit   string
	copyKey string
}{
	{entity.SketchMaxStrokes, "data-sketch-strokes", "stroke-limit"},
	{entity.SketchMaxPoints, "data-sketch-points", "point-limit"},
	{entity.SketchMaxCoordinate, "data-sketch-coordinate", ""},
	{entity.SketchMaxMilliseconds, "data-sketch-milliseconds", "time-limit"},
}

// TestTheSketchEditorHandsTheBrowserTheCeilingsTheValueDeclares reads a rendered drawing
// control the way its own script does: the board's fieldset carries the value's numbers as
// limits and the editor's copy of those same numbers as sentences. A control that ships a limit
// the value does not enforce refuses a legal drawing in the browser, or accepts one the doors
// refuse when the form is posted.
func TestTheSketchEditorHandsTheBrowserTheCeilingsTheValueDeclares(t *testing.T) {
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"},
	}).Render(&out); err != nil {
		t.Fatal(err)
	}
	board := onlyElementWith(t, out.String(), "data-sketch")
	for _, ceiling := range sketchCeilings {
		if got := attribute(board, ceiling.limit); got != strconv.FormatInt(ceiling.value, 10) {
			t.Errorf("%s = %q, want the value's own ceiling %d", ceiling.limit, got, ceiling.value)
		}
		if ceiling.copyKey == "" {
			continue
		}
		sentenceAttr := "data-sketch-" + ceiling.copyKey
		sentence := attribute(board, sentenceAttr)
		if sentence == "" {
			t.Errorf("the board carries no %s, so the editor's copy about that ceiling never reaches the page", sentenceAttr)
		} else if !namesFigure(sentence, ceiling.value) {
			t.Errorf("%s says %q, which names no ceiling of %d", sentenceAttr, sentence, ceiling.value)
		}
	}
}

// TestEverySketchCatalogueNamesTheCeilingTheValueDeclares asks the same question of every
// language the portable form controls ship, because a catalogue's words are what a person is
// shown in place of the English default the control falls back to.
func TestEverySketchCatalogueNamesTheCeilingTheValueDeclares(t *testing.T) {
	languages, err := fs.Glob(forms.Catalogues(), "*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(languages) == 0 {
		t.Fatal("the form controls ship no language catalogue")
	}
	for _, language := range languages {
		data, err := fs.ReadFile(forms.Catalogues(), language)
		if err != nil {
			t.Fatalf("%s catalogue: %v", language, err)
		}
		var messages map[string]struct {
			Translation string `json:"translation"`
		}
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatalf("%s catalogue: %v", language, err)
		}
		for _, ceiling := range sketchCeilings {
			if ceiling.copyKey == "" {
				continue
			}
			key := "forms.sketch." + ceiling.copyKey
			message, ok := messages[key]
			if !ok {
				t.Errorf("%s catalogue holds no %q, so a person reading that language is handed no sentence naming the ceiling of %d", language, key, ceiling.value)
				continue
			}
			if !namesFigure(message.Translation, ceiling.value) {
				t.Errorf("%s catalogue's %q says %q, which names no ceiling of %d", language, key, message.Translation, ceiling.value)
			}
		}
	}
}

// onlyElementWith returns the one element in a rendered control carrying an attribute, by name,
// and fails when the attribute is on none or on more than one. Presence is asked for, not value:
// the drawing board marks itself with an attribute that has no value.
func onlyElementWith(t *testing.T, source, name string) *html.Node {
	t.Helper()
	var found *html.Node
	carriers := 0
	for _, node := range elements(t, source) {
		for _, attr := range node.Attr {
			if attr.Key != name {
				continue
			}
			carriers++
			if carriers > 1 {
				t.Fatalf("%q is carried by %d elements of the rendered control, want one: %s", name, carriers, source)
			}
			found = node
		}
	}
	if found == nil {
		t.Fatalf("no element of the rendered control carries %q: %s", name, source)
	}
	return found
}

// grouping matches a thousands separator: 20,000 and 20 000 are one figure to the person
// reading it, and the two catalogues write it both ways.
var grouping = regexp.MustCompile(`(\d)[,. ](\d{3}\b)`)

// namesFigure reports whether text states value, however it groups the digits.
func namesFigure(text string, value int64) bool {
	for {
		ungrouped := grouping.ReplaceAllString(text, "$1$2")
		if ungrouped == text {
			break
		}
		text = ungrouped
	}
	return strings.Contains(text, strconv.FormatInt(value, 10))
}
