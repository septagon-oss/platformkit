package examples_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

func TestRefusedSpatialCapturesDoNotExportPriorNavigation(t *testing.T) {
	const privateURL = "/places?owner=tenant-one-private-id"
	refused := c.ContentState{Status: c.MediaRefused, Title: "Acesso recusado", Text: "Não pode ver os resultados."}
	info := examples.ExampleInfo{ID: "spatial/refused", ComponentID: "pk-ui.component.map-view"}
	check := func(t *testing.T, cleared, retained examples.Example) {
		t.Helper()
		clean, err := cleared.Describe()
		if err != nil || !strings.Contains(clean.HTML, refused.Text) {
			t.Fatalf("cleared localized refusal must render: %v", err)
		}
		capture, err := retained.Describe()
		if err != nil {
			return
		}
		payload, err := json.Marshal(capture)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(payload), privateURL) {
			t.Errorf("refused capture exported prior result URL in Props: %s", capture.Props)
		}
	}
	t.Run("map", func(t *testing.T) {
		cleared := c.MapViewProps{Label: "Locais", State: refused}
		retained := cleared
		retained.Views = []c.ChoiceLink{{Key: "private", Label: "Vista anterior", Href: privateURL}}
		check(t, examples.ExampleOf(info, cleared, c.MapView), examples.ExampleOf(info, retained, c.MapView))
	})
	t.Run("calendar", func(t *testing.T) {
		cleared := c.CalendarProps{Label: "Eventos", State: refused}
		retained := cleared
		retained.Previous = &c.ChoiceLink{Key: "private", Label: "Anterior", Href: privateURL}
		info.ComponentID = "pk-ui.component.calendar"
		check(t, examples.ExampleOf(info, cleared, c.Calendar), examples.ExampleOf(info, retained, c.Calendar))
	})
}
