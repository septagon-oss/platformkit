package examples_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components/examples"
)

// Every shared-family example written in English has a Portuguese twin, and
// the twin is the same story told in Portuguese: it renders and its copy is
// not the English copy again. Decision 0012 asks
// for both catalogs with the same keys; the Gallery is where these families
// keep theirs.
func TestEverySharedExampleInEnglishHasAPortugueseTwin(t *testing.T) {
	byID := map[string]examples.Example{}
	for _, example := range examples.Gallery() {
		byID[example.ID] = example
	}
	render := func(t *testing.T, example examples.Example) string {
		t.Helper()
		var out bytes.Buffer
		if err := example.Node.Render(&out); err != nil {
			t.Fatalf("%s does not render: %v", example.ID, err)
		}
		return out.String()
	}
	english := 0
	for id, example := range byID {
		stem, ok := strings.CutSuffix(id, "-en")
		if !ok || example.Group != "Shared workflows" {
			continue
		}
		english++
		t.Run(id, func(t *testing.T) {
			twin, ok := byID[stem+"-pt-PT"]
			if !ok {
				t.Fatalf("%s has no Portuguese twin %s-pt-PT", id, stem)
			}
			en, pt := render(t, example), render(t, twin)
			if en == strings.ReplaceAll(pt, "pt-PT", "en") {
				t.Fatalf("%s renders the English copy under a Portuguese name", twin.ID)
			}
		})
	}
	if english == 0 {
		t.Fatal("the Gallery has no English shared-family example to pair")
	}
}
