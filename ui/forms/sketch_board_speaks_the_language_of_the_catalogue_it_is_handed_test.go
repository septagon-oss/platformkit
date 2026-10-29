package forms_test

// A drawing board's words travel three seams: the control asks for each sentence under
// a key it builds itself, the shell hands that key to the request's catalogue, and the
// catalogue answers with a sentence or, holding no such entry, with the English the
// control carries as a default. Both sides of the names are read from the markup and
// from the catalogues elsewhere; what nothing asked is whether the key the control
// builds is the key the catalogue is filed under. A drift in that prefix or in one name
// leaves every attribute present and every catalogue complete, and a Portuguese page
// answering a person in English, because the board then holds the defaults it keeps for
// a shell that wires no words at all.
//
// This renders the board the way a page does — with the copy seam answering out of the
// served catalogue, as the resource screen wires it from the request's locale — and asks
// the rendered markup for that language's own sentence, in each language the portable
// form controls ship, and for no sentence of another.

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// TestTheSketchBoardSpeaksTheLanguageOfTheCatalogueItIsHanded renders one required
// drawing control per language the portable form controls ship, with the control's copy
// seam answering out of that language's catalogue, and asks the rendered markup for
// every sentence that catalogue keeps for the drawing field. A key the control asks
// under that no catalogue answers is handed to the person as the control's own English,
// whoever reads it, and a board that carries another language's sentence is not the
// board that language was written for.
func TestTheSketchBoardSpeaksTheLanguageOfTheCatalogueItIsHanded(t *testing.T) {
	languages, err := fs.Glob(forms.Catalogues(), "*.json")
	if err != nil || len(languages) < 2 {
		t.Fatalf("the portable form controls ship %d language catalogues, want one per language: %v", len(languages), err)
	}
	english := sketchLanguage(t, "en.json")
	if len(english) < 10 {
		t.Fatalf("the drawing control keeps %d sentences in en.json, too few to ask about", len(english))
	}

	for _, language := range languages {
		sentences := sketchLanguage(t, language)
		board := drawSketchBoard(t, sentences)
		for key, sentence := range sentences {
			if !strings.HasPrefix(key, "forms.sketch.") || strings.TrimSpace(sentence) == "" {
				continue
			}
			if !strings.Contains(board, sentence) {
				t.Errorf("%s board carries none of %q, the sentence this catalogue keeps for %s: the control asked for it under a key the catalogue never answered, so the board fell back to its own English", language, sentence, key)
			}
		}
		if language == "en.json" {
			continue
		}
		for key, sentence := range sentences {
			said := english[key]
			if !strings.HasPrefix(key, "forms.sketch.") || said == "" || said == sentence {
				continue
			}
			if strings.Contains(board, said) {
				t.Errorf("%s board answers %s in English: it carries %q, the sentence en.json keeps, where this catalogue says %q", language, key, said, sentence)
			}
		}
	}
}

// drawSketchBoard renders a required drawing control with its copy seam answering out
// of sentences, as the resource screen wires it from the request's locale, and returns
// the markup. Required, so the sentence a field adds when it must be filled is asked for
// too; the drawing board is the only control in this package that carries sentences of
// its own beyond its label.
func drawSketchBoard(t *testing.T, sentences map[string]string) string {
	t.Helper()
	var out strings.Builder
	err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch, Required: true}, Label: "Drawing"},
		Text: func(key, fallback string) string {
			if text := strings.TrimSpace(sentences[key]); text != "" {
				return text
			}
			return fallback
		},
	}).Render(&out)
	if err != nil {
		t.Fatalf("the drawing control could not render: %v", err)
	}
	return out.String()
}

// sketchLanguage is one served catalogue as key to text.
func sketchLanguage(t *testing.T, language string) map[string]string {
	t.Helper()
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
	texts := make(map[string]string, len(messages))
	for key, message := range messages {
		texts[key] = message.Translation
	}
	return texts
}
