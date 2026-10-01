package examples_test

import (
	"bytes"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestReviewRound2AbsentDetailsRefuseEveryProtectedSlot(t *testing.T) {
	child := examples.ExampleOf(examples.ExampleInfo{ID: "review/private-content", ComponentID: "pk-ui.component.text"},
		c.TextProps{Content: "private previous selection"}, c.Text)
	for _, kind := range []string{"side-panel", "detail-sheet"} {
		for _, status := range []c.MediaStatus{c.MediaLoading, c.MediaEmpty, c.MediaFailed, c.MediaRefused} {
			t.Run(kind+"/"+string(status), func(t *testing.T) {
				p := c.DetailPanelProps{
					ComponentProps: c.ComponentProps{ID: "review-detail"},
					Label:          "Detalhes", CloseLabel: "Voltar", ReturnHref: "/items",
					State: c.ContentState{Status: status, Title: "Estado", Text: "Não existem dados disponíveis.", LoadingLabel: "A carregar"},
				}
				example := func(slots c.DetailPanelSlots) examples.Example {
					info := examples.ExampleInfo{ID: "review/cleared-detail", ComponentID: "pk-ui.component." + kind}
					if kind == "side-panel" {
						return examples.ExampleWithSlots(info, p, slots, c.SidePanelWithSlots)
					}
					return examples.ExampleWithSlots(info, c.DetailSheetProps{DetailPanelProps: p}, slots, c.DetailSheetWithSlots)
				}
				cleared, err := example(c.DetailPanelSlots{}).Describe()
				if err != nil || !strings.Contains(cleared.HTML, "review-detail") {
					t.Fatalf("valid cleared state must reach the renderer: %v", err)
				}
				for name, slots := range map[string]c.DetailPanelSlots{
					"header":  {Header: []g.Node{child.Node}},
					"body":    {Body: []g.Node{child.Node}},
					"actions": {Actions: []g.Node{child.Node}},
				} {
					t.Run(name, func(t *testing.T) {
						candidate := example(slots)
						var out bytes.Buffer
						if err := candidate.Node.Render(&out); err == nil || out.Len() != 0 {
							t.Fatalf("retained protected slot must refuse before bytes: %v, %q", err, out.String())
						}
						if _, err := candidate.Describe(); err == nil {
							t.Fatal("retained protected slot must refuse typed capture")
						}
					})
				}
			})
		}
	}
}
