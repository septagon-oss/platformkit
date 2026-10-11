package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestListVisibilityAlsoAppliesToTheDefaultPrimaryField(t *testing.T) {
	for _, visibility := range []string{"hidden", "detail"} {
		t.Run(visibility, func(t *testing.T) {
			r := note()
			r.Schema.Fields = fieldsWith(map[string]entity.FieldHints{"title": {Visibility: visibility}})
			row := map[string]any{"id": "1", "title": "Integration-only identity", "status": "open"}
			out := render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body)
			if strings.Contains(out, "Integration-only identity") {
				t.Fatalf("visibility:%s still exposes the default primary field on the list", visibility)
			}
		})
	}
}
