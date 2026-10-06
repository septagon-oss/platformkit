package legible_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

func TestComposedCopyKeepsUntranslatedWordsVisible(t *testing.T) {
	const tenant = "Tenant Name"
	for _, tc := range []struct {
		name, title string
		copy, data  int
	}{
		{name: "translated heading and tenant", title: pseudo.Mark("New task") + " · " + tenant},
		{name: "typed tenant", title: tenant, data: 1},
		{name: "numeric value and tenant", title: "42 · " + tenant, data: 1},
		{name: "untranslated heading and tenant", title: "New task · " + tenant, copy: 1},
		{name: "untranslated suffix", title: pseudo.Mark("New task") + " · Save now", copy: 1},
		{name: "copy around typed value", title: "Delete " + tenant, copy: 1},
		{name: "unrecognised value", title: "Another tenant", copy: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			collected, err := legible.Scan([]byte("<title>"+tc.title+"</title>"), pseudo.Wrapped)
			if err != nil {
				t.Fatal(err)
			}
			if len(collected) != 1 || collected[0].Text != tc.title {
				t.Fatalf("title did not reach the scan: %+v", collected)
			}
			original := slices.Clone(collected)
			marked := legible.MarkDatum(collected, pseudo.Wrapped, []string{tenant})
			copy, data := legible.Report("GET /tasks/new", marked)
			if len(copy) != tc.copy || len(data) != tc.data {
				t.Errorf("copy=%v data=%v; want %d untranslated and %d data strings", copy, data, tc.copy, tc.data)
			}
			for _, line := range copy {
				if !strings.HasPrefix(line, "TEXT GET /tasks/new html[1] > head[1] > title[1]#text ") {
					t.Errorf("untranslated title lost its page or tree path: %s", line)
				}
			}
			if !slices.Equal(collected, original) {
				t.Errorf("data classification changed the caller's scan: got %+v, want %+v", collected, original)
			}
		})
	}
}
