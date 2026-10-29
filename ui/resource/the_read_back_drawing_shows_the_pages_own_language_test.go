package resource_test

// A drawing is read in two places a person never edits: the swatch in a row and the
// wide drawing on the record. Both are drawn by ui/components, which keeps two
// sentences of its own — the line beside a drawing nobody has saved, and the line
// beside one the server could not read — and ui/resource puts each of them to the
// request's copy seam. Whether that seam reaches the board a person *edits* is asked
// in ui/forms; nothing asked it of the reading, so a call site that carried either
// sentence as a literal, or stopped passing the seam, left an English "No drawing." on
// a Portuguese record: one page answering in two languages, with every markup
// assertion beside it still true.
//
// This renders both reading places in every language the portable form controls ship,
// through the seam a request carries, and asks each for that language's own sentence
// for both lines. A language other than English is refused for the English wording
// too, because the English is what a dropped seam hands back.

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// sketchReadingKeys are the two sentences the reading of a drawing says, and the
// drawing each is asked of: a row saved with nothing drawn, and a value the server
// could not read back.
var sketchReadingKeys = []struct{ key, raw string }{
	{"forms.sketch.empty", "[]"},
	{"forms.sketch.unavailable", "not a drawing"},
}

// TestTheReadBackDrawingShowsThePagesOwnLanguage renders one drawing
// field as a row's swatch and as a record's wide reading in each language the portable
// form controls ship, and asks the markup for that language's own sentence for an
// empty drawing and for one that could not be read — and, in a language other than
// English, for none of the English catalogue's wording of the same line.
func TestTheReadBackDrawingShowsThePagesOwnLanguage(t *testing.T) {
	languages, err := fs.Glob(forms.Catalogues(), "*.json")
	if err != nil || len(languages) < 2 {
		t.Fatalf("the portable form controls ship %d language catalogues, want one per language: %v", len(languages), err)
	}
	kept := map[string]map[string]string{}
	for _, file := range languages {
		kept[file] = sketchCatalogueSentences(t, file)
	}
	for _, file := range languages {
		language := strings.TrimSuffix(file, ".json")
		show := resource.Options{Workspace: "/app", Home: "Dashboard",
			Locale: speak(language, spoken{words: kept[file]})}
		r := resource.Resource{Screen: "/app/drawing/drawings", Schema: entity.Schema{
			Module: "drawing", Entity: "drawing", Path: "/api/v1/drawing/drawings",
			Fields: []entity.Field{{Name: "id", Type: entity.TypeUUID, ReadOnly: true},
				{Name: "drawing", Type: entity.TypeSketch}}}}
		row := map[string]any{"id": "1", "drawing": "[]"}
		for _, asked := range sketchReadingKeys {
			row["drawing"] = asked.raw
			for _, place := range []struct {
				name string
				body func() string
			}{
				{"the row's swatch", func() string {
					return render(t, resource.List(r, show, []map[string]any{row}, 1, 1, "", false).Body)
				}},
				{"the record's reading", func() string {
					return render(t, resource.Detail(r, show, row, false).Body)
				}},
			} {
				body := place.body()
				said := kept[file][asked.key]
				if !strings.Contains(body, said) {
					t.Errorf("%s of a drawing on a %s page shows none of %q, the sentence %s keeps for %s: the reading carries its own English the moment it stops asking the request, so one page answers in two languages",
						place.name, language, said, file, asked.key)
				}
				if file == "en.json" {
					continue
				}
				if en := kept["en.json"][asked.key]; en != said && strings.Contains(body, en) {
					t.Errorf("%s of a drawing on a %s page shows %q, the English catalogue's own wording of %s: only a seam that was never asked answers in the control's language here",
						place.name, language, en, asked.key)
				}
			}
		}
	}
}

// sketchCatalogueSentences is what one served catalogue keeps for the two sentences a
// drawing's reading says. A catalogue that holds neither is a catalogue this question
// cannot be asked of, so it fails rather than passing on nothing asked.
func sketchCatalogueSentences(t *testing.T, file string) map[string]string {
	t.Helper()
	data, err := fs.ReadFile(forms.Catalogues(), file)
	if err != nil {
		t.Fatalf("%s catalogue: %v", file, err)
	}
	var served map[string]struct{ Translation string }
	if err := json.Unmarshal(data, &served); err != nil {
		t.Fatalf("%s catalogue: %v", file, err)
	}
	out := map[string]string{}
	for _, asked := range sketchReadingKeys {
		said := strings.TrimSpace(served[asked.key].Translation)
		if said == "" {
			t.Fatalf("%s keeps no sentence for %s, so the reading of a drawing cannot be asked of it", file, asked.key)
		}
		out[asked.key] = said
	}
	return out
}
