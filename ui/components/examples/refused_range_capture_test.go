package examples_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

// A failed or refused read keeps no route into the result it no longer has. The
// rule Calendar and MapView apply holds for every component whose Props carry a
// range control or a strip of days beside an absent result.
func TestRefusedRangeCapturesDoNotExportPriorNavigation(t *testing.T) {
	const privateURL = "/slots?owner=tenant-one-private-id"
	refused := c.ContentState{Status: c.MediaRefused, Title: "Acesso recusado", Text: "Não pode ver os resultados."}
	private := c.ChoiceLink{Key: "private", Label: "Anterior", Href: privateURL}
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
	t.Run("slot-picker", func(t *testing.T) {
		info := examples.ExampleInfo{ID: "range/refused", ComponentID: "pk-ui.component.slot-picker"}
		cleared := c.SlotPickerProps{Label: "Horários", State: refused}
		retained := cleared
		retained.DateStrip.Previous = &private
		check(t, examples.ExampleOf(info, cleared, c.SlotPicker), examples.ExampleOf(info, retained, c.SlotPicker))
	})
	t.Run("slot-picker-days", func(t *testing.T) {
		info := examples.ExampleInfo{ID: "range/refused", ComponentID: "pk-ui.component.slot-picker"}
		cleared := c.SlotPickerProps{Label: "Horários", State: refused}
		retained := cleared
		retained.DateStrip = c.DateStripProps{Label: "Dias", SelectedDate: "2026-10-05", Days: []c.DateChoice{{Date: "2026-10-05", Label: "5 out", Href: privateURL}}}
		check(t, examples.ExampleOf(info, cleared, c.SlotPicker), examples.ExampleOf(info, retained, c.SlotPicker))
	})
	t.Run("area-chart", func(t *testing.T) {
		info := examples.ExampleInfo{ID: "range/refused", ComponentID: "pk-ui.component.area-chart"}
		cleared := c.AreaChartProps{Label: "Receita", State: refused}
		retained := cleared
		retained.Ranges = []c.ChoiceLink{private}
		check(t, examples.ExampleOf(info, cleared, c.AreaChart), examples.ExampleOf(info, retained, c.AreaChart))
	})
}
