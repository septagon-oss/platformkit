package forms_test

// The drawing board is two files written apart from each other: the markup this
// package renders, and the controller the shell serves beside it, which binds by
// attribute name and reads every sentence it speaks off that markup. Nothing compiles
// the pair together, and the one case that drives a real browser runs only where a
// person names an installed Playwright, so a rename on either side is a board whose
// Undo and Clear answer nothing while the suites stay green. This reads the handles,
// the bounds and the announcements out of the script the shell ships and asks the
// rendered board for every one of them, in both languages it can be shown in.

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// sketchScript is the controller as the shell serves it: the copy in the asset tree
// rather than a fixture, because these two drifting apart is what is asked here.
const sketchScript = "js/sketch.js"

var (
	// sketchHandle matches a quoted attribute selector the controller binds by:
	// '[data-sketch]', '[data-sketch-value]' or '[data-sketch-action="undo"]'.
	sketchHandle = regexp.MustCompile(`'\[data-sketch([a-z-]*)(?:="([a-z-]+)")?\]'`)
	// sketchElement matches the one element the controller asks for by tag name.
	sketchElement = regexp.MustCompile(`querySelector\('([a-z]+)'\)`)
	// sketchSaid matches one sentence the controller reads off the board, said on its
	// own or announced as the last act of the change that saved the drawing.
	sketchSaid = regexp.MustCompile(`\.(?:say|change)\('([a-z-]+)'\)`)
	// sketchCeilingList matches the names of the bounds the controller reads off the
	// board before it accepts a drawing; quotedName picks them out of it.
	sketchCeilingList = regexp.MustCompile(`\[('[a-z-]+'(?:,\s*'[a-z-]+')*)\]\.map\(key =>`)
	quotedName        = regexp.MustCompile(`'([a-z-]+)'`)
)

// TestTheSketchBoardCarriesEveryHandleItsScriptBinds renders a drawing control and
// asks it the way its own controller does: every element and attribute it selects by
// name stands inside the board it mounts on. A handle the markup dropped leaves the
// controller mounted on half a board — no live region, no button, a preview it can no
// longer hide — with the form still posting whatever the input last held.
func TestTheSketchBoardCarriesEveryHandleItsScriptBinds(t *testing.T) {
	script, _, within := sketchBoard(t)

	binds := sketchHandle.FindAllStringSubmatch(script, -1)
	if len(binds) < 5 {
		t.Fatalf("the shipped controller selects %d board handles, too few to ask the markup about: %s", len(binds), script)
	}
	for _, match := range binds {
		want := ""
		if match[2] != "" {
			want = `="` + match[2] + `"`
		}
		if !carries(within, "data-sketch"+match[1], match[2]) {
			t.Errorf("the controller binds [data-sketch%s%s], which no element of the rendered board carries", match[1], want)
		}
	}
	for _, match := range sketchElement.FindAllStringSubmatch(script, -1) {
		var found bool
		for _, node := range within {
			found = found || node.Data == match[1]
		}
		if !found {
			t.Errorf("the controller asks the board for a <%s>, which the rendered markup holds none of", match[1])
		}
	}
}

// TestTheSketchBoardCarriesEverySentenceItsScriptSpeaks asks for the words
// themselves. The controller keeps no copy: it reads each status and each ceiling
// warning off the board by a name it chooses, and the portable form controls hand that
// markup the words from a catalogue per language. A board missing one attribute says
// nothing when the drawing breaks its ceiling, and a catalogue holding no key for it
// answers a person in English whoever they are reading as.
func TestTheSketchBoardCarriesEverySentenceItsScriptSpeaks(t *testing.T) {
	script, board, _ := sketchBoard(t)
	said, ceilings := sentences(script), ceilingNames(script)
	if len(said) < 5 || len(ceilings) == 0 {
		t.Fatalf("the shipped controller speaks %d sentences and reads %d ceilings, too few to ask about", len(said), len(ceilings))
	}
	catalogues := sketchCatalogues(t)

	for _, key := range append(said, ceilings...) {
		if attribute(board, "data-sketch-"+key) == "" {
			t.Errorf("the board carries no data-sketch-%q, which the controller reads off it", key)
		}
	}
	for _, key := range said {
		for language, catalogue := range catalogues {
			if strings.TrimSpace(catalogue["forms.sketch."+key]) == "" {
				t.Errorf("%s catalogue holds no sentence for forms.sketch.%q, which the controller announces", language, key)
			}
		}
		if portu, english := catalogues["pt-PT.json"]["forms.sketch."+key], catalogues["en.json"]["forms.sketch."+key]; portu != "" && portu == english {
			t.Errorf("pt-PT catalogue repeats the English sentence %q for forms.sketch.%q rather than translating it", english, key)
		}
	}
}

// sketchBoard renders one drawing control and returns the shipped controller, the
// element the controller mounts on, and every element standing inside it.
func sketchBoard(t *testing.T) (string, *html.Node, []*html.Node) {
	t.Helper()
	var out strings.Builder
	if err := forms.Control(forms.ControlProps{
		Field: forms.Field{Definition: entity.Field{Name: "drawing", Type: entity.TypeSketch}, Label: "Drawing"},
	}).Render(&out); err != nil {
		t.Fatal(err)
	}
	board := onlyElementWith(t, out.String(), "data-sketch")
	var within strings.Builder
	if err := html.Render(&within, board); err != nil {
		t.Fatal(err)
	}
	script, err := fs.ReadFile(ui.Assets(ui.Compose(design.Default())), sketchScript)
	if err != nil {
		t.Fatalf("no %s among the assets a shell serves: %v", sketchScript, err)
	}
	return string(script), board, elements(t, within.String())
}

// sentences names the announcements the controller reads off the board, in the order
// it speaks them: said on their own, or announced as the last act of the save.
func sentences(script string) []string {
	var found []string
	for _, match := range sketchSaid.FindAllStringSubmatch(script, -1) {
		if !slices.Contains(found, match[1]) {
			found = append(found, match[1])
		}
	}
	return found
}

// ceilingNames names the bounds the controller reads off the board before it accepts a
// drawing.
func ceilingNames(script string) []string {
	match := sketchCeilingList.FindStringSubmatch(script)
	if match == nil {
		return nil
	}
	var found []string
	for _, name := range quotedName.FindAllStringSubmatch(match[1], -1) {
		found = append(found, name[1])
	}
	return found
}

// sketchCatalogues is every language the portable form controls ship, as key to text.
func sketchCatalogues(t *testing.T) map[string]map[string]string {
	t.Helper()
	languages, err := fs.Glob(forms.Catalogues(), "*.json")
	if err != nil || len(languages) == 0 {
		t.Fatalf("the portable form controls ship no language catalogues: %v", err)
	}
	out := map[string]map[string]string{}
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
		texts := make(map[string]string, len(messages))
		for key, message := range messages {
			texts[key] = message.Translation
		}
		out[language] = texts
	}
	return out
}

// carries reports whether any of these elements holds the attribute — by name alone
// when the controller selects it that way, by name and value when it names one.
func carries(nodes []*html.Node, name, want string) bool {
	for _, node := range nodes {
		for _, attr := range node.Attr {
			if attr.Key == name && (want == "" || attr.Val == want) {
				return true
			}
		}
	}
	return false
}
