package examples_test

import (
	"encoding/json"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestRefusedDetailCaptureDoesNotExportOmittedProtectedChildren(t *testing.T) {
	const secret = "private-body-from-the-previous-selection"
	child := examples.ExampleOf(examples.ExampleInfo{ID: "review/previous-body", ComponentID: "pk-ui.component.text"},
		c.TextProps{Content: secret}, c.Text)
	p := c.DetailPanelProps{
		ComponentProps: c.ComponentProps{ID: "review-detail"},
		Label:          "Detalhes", CloseLabel: "Voltar", ReturnHref: "/items",
		State: c.ContentState{Status: c.MediaRefused, Title: "Indisponível", Text: "Não tem acesso."},
	}
	info := examples.ExampleInfo{ID: "review/refused-panel", ComponentID: "pk-ui.component.side-panel"}
	// Cleared refusal is a valid, localized state and proves the test can reach
	// the capture API without depending on the leaked value being present.
	cleared, err := examples.ExampleWithSlots(info, p, c.DetailPanelSlots{}, c.SidePanelWithSlots).Describe()
	if err != nil || !strings.Contains(cleared.HTML, "Não tem acesso.") {
		t.Fatalf("cleared refusal must render: %v", err)
	}
	description, err := examples.ExampleWithSlots(info, p,
		c.DetailPanelSlots{Body: []g.Node{child.Node}}, c.SidePanelWithSlots).Describe()
	if err != nil {
		return // Refusing a retained protected slot before export is safe too.
	}
	payload, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), secret) {
		t.Fatalf("a refused panel exported its omitted protected body in capture JSON children: %+v", description.Children)
	}
}
