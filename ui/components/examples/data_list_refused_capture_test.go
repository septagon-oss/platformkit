package examples_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestRefusedDataListCaptureExcludesStaleControls(t *testing.T) {
	info := examples.ExampleInfo{ID: "records/refused", ComponentID: "pk-ui.component.data-list"}
	props := c.DataListProps{Label: "Records", State: c.ContentState{
		Status: c.MediaRefused, Title: "Access refused", Text: "You cannot read these records.",
	}}
	cleared, err := examples.ExampleWithSlots(info, props, c.DataListSlots{}, c.DataListWithSlots).Describe()
	if err != nil || !strings.Contains(cleared.HTML, "Access refused") {
		t.Fatalf("a cleared refusal must reach typed capture: %v", err)
	}

	props.Filters = []c.ChoiceLink{{Key: "owner", Label: "Private owner", Href: "/records?owner=private-owner-id"}}
	slots := c.DataListSlots{Toolbar: []g.Node{g.Text("Private workspace")}}
	description, err := examples.ExampleWithSlots(info, props, slots, c.DataListWithSlots).Describe()
	if err != nil {
		return // Rejecting retained controls before export is safe.
	}
	payload, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	owner := strings.Contains(string(payload), "Private owner")
	workspace := strings.Contains(string(payload), "Private workspace")
	identifier := strings.Contains(string(payload), "private-owner-id")
	if owner || workspace || identifier {
		t.Fatalf("refused capture retained stale controls: owner=%t workspace=%t identifier=%t", owner, workspace, identifier)
	}
}
