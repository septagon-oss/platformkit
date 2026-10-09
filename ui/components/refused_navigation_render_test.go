package components_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func TestRefusedSpatialViewsDoNotRenderPriorNavigation(t *testing.T) {
	const privateURL = "/places?owner=tenant-one-private-id"
	refused := c.ContentState{
		Status: c.MediaRefused, Title: "Acesso recusado", Text: "Não pode ver os resultados.",
	}
	check := func(t *testing.T, cleared, retained g.Node) {
		t.Helper()
		var cleanHTML strings.Builder
		if err := cleared.Render(&cleanHTML); err != nil || !strings.Contains(cleanHTML.String(), refused.Text) {
			t.Fatalf("cleared refusal did not render its supplied message: %v", err)
		}
		var oldHTML strings.Builder
		err := retained.Render(&oldHTML)
		if err != nil && oldHTML.Len() != 0 {
			t.Fatalf("retained navigation wrote partial output before refusal: %v", err)
		}
		if strings.Contains(oldHTML.String(), privateURL) {
			t.Fatalf("refused view rendered prior result URL %q", privateURL)
		}
	}

	t.Run("map", func(t *testing.T) {
		cleared := c.MapViewProps{Label: "Locais", State: refused}
		retained := cleared
		retained.Views = []c.ChoiceLink{{Key: "private", Label: "Vista anterior", Href: privateURL}}
		check(t, c.MapView(cleared), c.MapView(retained))
	})

	t.Run("calendar", func(t *testing.T) {
		cleared := c.CalendarProps{Label: "Eventos", State: refused}
		retained := cleared
		retained.Previous = &c.ChoiceLink{Key: "private", Label: "Anterior", Href: privateURL}
		check(t, c.Calendar(cleared), c.Calendar(retained))
	})
}
