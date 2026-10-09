package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestExplicitShownVisibilityOverridesHideListInTheList(t *testing.T) {
	r := note()
	r.Schema.Fields = fieldsWith(map[string]entity.FieldHints{"body": {Visibility: "shown"}})
	row := map[string]any{"id": "1", "title": "Visible note", "body": "Explicitly shown body"}
	out := render(t, resource.List(r, opts, []map[string]any{row}, 1, 1, "", false).Body)
	if !strings.Contains(out, "Explicitly shown body") {
		t.Fatal("the list hides a field whose explicit visibility is shown")
	}
}
