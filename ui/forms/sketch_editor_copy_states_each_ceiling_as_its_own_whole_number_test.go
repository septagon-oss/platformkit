package forms_test

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// sketchCeilingCopy lists each ceiling the drawing value enforces beside the copy key of the
// sentence the editor shows a person when a drawing breaks that limit.
var sketchCeilingCopy = []struct {
	value   int64
	copyKey string
}{
	{entity.SketchMaxStrokes, "stroke-limit"},
	{entity.SketchMaxPoints, "point-limit"},
	{entity.SketchMaxMilliseconds, "time-limit"},
}

// sketchCeilingSentence pairs one sentence the editor hands over about a ceiling with the ceiling
// it describes and where the sentence came from.
type sketchCeilingSentence struct {
	source   string
	sentence string
	ceiling  int64
}

// sketchCeilingSentences collects every sentence the editor hands over about those ceilings: the
// attribute on the rendered board carries the control's own words, and each language catalogue the
// form controls ship carries the words that replace those defaults for a person reading it.
func sketchCeilingSentences(t *testing.T) []sketchCeilingSentence {
	t.Helper()
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"},
	}).Render(&out); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	languages, err := fs.Glob(forms.Catalogues(), "*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(languages) == 0 {
		t.Fatal("the form controls ship no language catalogue")
	}
	var sentences []sketchCeilingSentence
	for _, ceiling := range sketchCeilingCopy {
		key := "forms.sketch." + ceiling.copyKey
		sentences = append(sentences, sketchCeilingSentence{
			source:   "the rendered control's " + key,
			sentence: renderedAttribute(t, rendered, "data-sketch-"+ceiling.copyKey),
			ceiling:  ceiling.value,
		})
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
			message, ok := messages[key]
			if !ok {
				t.Errorf("%s catalogue holds no %q", language, key)
				continue
			}
			sentences = append(sentences, sketchCeilingSentence{
				source:   language + " catalogue's " + key,
				sentence: message.Translation,
				ceiling:  ceiling.value,
			})
		}
	}
	return sentences
}

// renderedAttribute returns the one value the rendered markup carries for name, and fails the
// test when it appears on none or on more than one element.
func renderedAttribute(t *testing.T, markup, name string) string {
	t.Helper()
	values := regexp.MustCompile(regexp.QuoteMeta(name)+`="([^"]*)"`).FindAllStringSubmatch(markup, -1)
	if len(values) != 1 {
		t.Fatalf("%q appears on %d elements of the rendered control, want one: %s", name, len(values), markup)
	}
	return values[0][1]
}

// TestEachSketchEditorSentenceStatesItsCeilingAsAWholeNumberOfItsOwn asks that a sentence about a
// drawing's limit write that limit as a number standing on its own. A person counting strokes acts
// on the figure they were told, so digits that merely begin with the enforced bound - a sentence
// reading "at most 4000 strokes" beside a field that refuses its four-hundred-and-first - send a
// person to undo work the field never asked for, or to keep a board every writing door refuses.
// Digit grouping is folded first, since "20,000" and "20 000" are one figure to the reader.
func TestEachSketchEditorSentenceStatesItsCeilingAsAWholeNumberOfItsOwn(t *testing.T) {
	for _, said := range sketchCeilingSentences(t) {
		if said.sentence == "" {
			t.Errorf("%s carries no sentence", said.source)
			continue
		}
		figure := strconv.FormatInt(said.ceiling, 10)
		if !statesFigureAline(ungroupDigits(said.sentence), figure) {
			t.Errorf("%s says %q, which states no number of its own equal to the ceiling %s", said.source, said.sentence, figure)
		}
	}
}

// digitSeparators matches a thousands separator between two digits: a comma, a full stop or a
// space, which a reader of the sentence ignores.
var digitSeparators = regexp.MustCompile(`([0-9])[,. ]+([0-9])`)

// ungroupDigits removes the separators between digits, repeatedly, so a catalogue may write one
// figure either way.
func ungroupDigits(text string) string {
	for {
		next := digitSeparators.ReplaceAllString(text, "$1$2")
		if next == text {
			return text
		}
		text = next
	}
}

// digitRun matches one maximal run of digits in text whose separators are folded.
var digitRun = regexp.MustCompile(`[0-9]+`)

// statesFigureAline reports whether text states figure as a run of digits of its own, rather than
// as the leading digits of a longer one.
func statesFigureAline(text, figure string) bool {
	for _, run := range digitRun.FindAllString(text, -1) {
		if run == figure {
			return true
		}
	}
	return false
}
