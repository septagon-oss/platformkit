package components_test

import (
	"bytes"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestFailedDataListRejectsRetainedResultControls(t *testing.T) {
	failed := c.ContentState{Status: c.MediaFailed, Title: "Não foi possível carregar", Text: "Tente novamente."}
	props := c.DataListProps{Label: "Registos", State: failed}
	info := examples.ExampleInfo{ID: "records/failed", ComponentID: "pk-ui.component.data-list"}
	var cleared bytes.Buffer
	if err := c.DataListWithSlots(props, c.DataListSlots{RetryAction: []g.Node{g.Text("Tentar novamente")}}).Render(&cleared); err != nil || !strings.Contains(cleared.String(), failed.Text) {
		t.Fatalf("a cleared failure must show the request's recovery copy: %v, %q", err, cleared.String())
	}

	props.ResultKey = "prior-tenant-result"
	props.Filters = []c.ChoiceLink{{Key: "owner", Label: "Prior tenant", Href: "/records?owner=prior-tenant"}}
	slots := c.DataListSlots{Toolbar: []g.Node{g.Text("Prior tenant controls")}}
	t.Run("render", func(t *testing.T) {
		var out bytes.Buffer
		if err := c.DataListWithSlots(props, slots).Render(&out); err == nil || out.Len() != 0 {
			t.Fatalf("failed read retained an earlier result or controls: %v, %q", err, out.String())
		}
	})
	t.Run("capture", func(t *testing.T) {
		if _, err := examples.ExampleWithSlots(info, props, slots, c.DataListWithSlots).Describe(); err == nil {
			t.Fatal("failed read exported an earlier result or controls")
		}
	})
}
