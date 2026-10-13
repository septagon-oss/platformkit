package legible_test

import (
	"html"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

func TestEncodedAttributeCopyKeepsItsDecodedTextAndElementLocation(t *testing.T) {
	const page = "GET /tasks/new"
	const tenant = `Tenant "A" & B`
	text := tenant + " · " + pseudo.Mark("Open <draft> — save | close") + ` · Save "draft" & continue`
	for _, attribute := range []string{"alt", "title", "aria-label", "placeholder"} {
		t.Run(attribute, func(t *testing.T) {
			for _, prefix := range []string{"", "\n<!-- explanation -->\n"} {
				body := `<main><hr>` + prefix + `<input ` + attribute + `="` + html.EscapeString(text) + `"></main>`
				collected, err := legible.Scan([]byte(body), pseudo.Wrapped)
				if err != nil || len(collected) != 1 {
					t.Fatalf("Scan = %v, %v; want one attribute", collected, err)
				}
				classified := legible.MarkDatum(collected, pseudo.Wrapped, []string{tenant})
				copy, data := legible.Report(page, classified)
				want := `TEXT ` + page + ` html[1] > body[2] > main[1] > input[2] "` + text + `" in ` + attribute
				if len(copy) != 1 || copy[0] != want || len(data) != 0 {
					t.Fatalf("Report = %v, %v; want %q and no data", copy, data, want)
				}
				if collected[0].Text != text || collected[0].Reached || collected[0].Datum {
					t.Fatalf("original decoded attribute changed: %+v", collected[0])
				}
			}
		})
	}
}
