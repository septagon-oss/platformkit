package legible_test

import (
	"html"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

func TestComposedAttributesKeepUnmarkedCopyVisible(t *testing.T) {
	const tenant = "Tenant Name"
	marked := pseudo.Mark("Open ⟦draft⟧ — then save | close")
	for _, attribute := range legible.Attributes {
		for _, suffix := range []string{tenant, "Save without translation"} {
			t.Run(attribute+"/"+suffix, func(t *testing.T) {
				text := tenant + " · " + marked + " · " + suffix
				document := `<input ` + attribute + `="` + html.EscapeString(text) + `">`
				collected, err := legible.Scan([]byte(document), pseudo.Wrapped)
				if err != nil || len(collected) != 1 {
					t.Fatalf("attribute did not reach the scan: %v, %v", collected, err)
				}
				classified := legible.MarkDatum(collected, pseudo.Wrapped, []string{tenant})
				copy, data := legible.Report("GET /tasks/edit", classified)
				if suffix == tenant {
					if !classified[0].Reached || len(copy) != 0 || len(data) != 0 {
						t.Fatalf("marked sentence and typed values lost their classification: %v %v %v", classified, copy, data)
					}
				} else if len(copy) != 1 || len(data) != 0 || !strings.Contains(copy[0], suffix) || !strings.Contains(copy[0], "in "+attribute) {
					t.Fatalf("unmarked attribute copy lost its diagnostic: %v %v", copy, data)
				}
			})
		}
	}
}
