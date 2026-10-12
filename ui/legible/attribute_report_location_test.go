package legible_test

import (
	"html"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

func TestUnmarkedAttributeCopyKeepsItsPageAndTreeLocation(t *testing.T) {
	const tenant = "Tenant Name"
	const page = "GET /tasks/edit"
	for _, attribute := range []string{"alt", "title", "aria-label", "placeholder"} {
		t.Run(attribute, func(t *testing.T) {
			text := tenant + " · " + pseudo.Mark("Open ⟦draft⟧ — then save | close") + " · Save without translation"
			body := `<main><input ` + attribute + `="` + html.EscapeString(text) + `"></main>`
			collected, err := legible.Scan([]byte(body), pseudo.Wrapped)
			if err != nil || len(collected) != 1 {
				t.Fatalf("attribute collection: %v, %v", collected, err)
			}
			original := slices.Clone(collected)
			classified := legible.MarkDatum(collected, pseudo.Wrapped, []string{tenant})
			copy, data := legible.Report(page, classified)
			want := `TEXT ` + page + ` html[1] > body[2] > main[1] > input[1] "` + text + `" in ` + attribute
			if len(copy) != 1 || copy[0] != want || len(data) != 0 {
				t.Fatalf("report = %v, data = %v; want %q and no data", copy, data, want)
			}
			if !reflect.DeepEqual(collected, original) {
				t.Fatalf("classifying typed values changed the original scan: %v", collected)
			}
		})
	}
}
