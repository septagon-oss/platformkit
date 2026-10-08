package components_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func TestRefusedDataListDoesNotRenderRetainedControls(t *testing.T) {
	props := c.DataListProps{
		Label:   "Records",
		State:   c.ContentState{Status: c.MediaRefused, Title: "Access refused", Text: "You cannot read these records."},
		Filters: []c.ChoiceLink{{Key: "owner", Label: "Confidential owner", Href: "/records?owner=private"}},
	}
	cleared := props
	cleared.Filters = nil
	var valid strings.Builder
	if err := c.DataList(cleared).Render(&valid); err != nil || !strings.Contains(valid.String(), "Access refused") {
		t.Fatalf("a cleared refusal must render before checking retained controls: %v", err)
	}
	slots := c.DataListSlots{Toolbar: []g.Node{g.Text("Confidential workspace")}}

	var out strings.Builder
	err := c.DataListWithSlots(props, slots).Render(&out)
	if err != nil && out.Len() != 0 {
		t.Fatalf("refused composition wrote partial output before the error: %q, %v", out.String(), err)
	}
	markup := out.String()
	toolbar := strings.Contains(markup, "Confidential workspace")
	filter := strings.Contains(markup, "Confidential owner")
	identifier := strings.Contains(markup, "owner=private")
	if toolbar || filter || identifier {
		t.Fatalf("refused list disclosed retained controls: toolbar=%t filter=%t identifier=%t", toolbar, filter, identifier)
	}
}
