package components_test

import (
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func panelFixture() c.DetailPanelProps {
	return c.DetailPanelProps{ComponentProps: c.ComponentProps{ID: "detail"}, ItemID: "a", Title: "Item A", Label: "Item details", CloseLabel: "Return to items", ReturnHref: "/items"}
}

func TestDetailPanelsReuseTheFrameWithDistinctSemantics(t *testing.T) {
	p := panelFixture()
	slots := c.DetailPanelSlots{Body: []g.Node{g.Text("Body")}, Actions: []g.Node{g.Text("Actions")}}
	sheet := html(t, c.DetailSheetWithSlots(c.DetailSheetProps{DetailPanelProps: p, Open: true}, slots))
	side := html(t, c.SidePanelWithSlots(p, slots))
	for _, out := range []string{sheet, side} {
		for _, want := range []string{`data-modal-panel`, `data-modal-body`, `data-modal-footer`, `data-detail-item="a"`, `href="/items"`, "Body", "Actions"} {
			if !strings.Contains(out, want) {
				t.Fatalf("P7: missing %s: %s", want, out)
			}
		}
	}
	if !strings.Contains(sheet, `<dialog`) || !strings.Contains(sheet, `aria-modal="true"`) || !strings.Contains(side, `<aside`) || strings.Contains(side, `aria-modal`) {
		t.Fatal("P7: sheet and side-panel semantics conflated")
	}
}

func TestDetailPanelsRefuseOldContentOnAbsentStates(t *testing.T) {
	p := panelFixture()
	p.State = c.ContentState{Status: c.MediaRefused, Title: "Unavailable", Text: "No access"}
	var raw strings.Builder
	if err := c.SidePanel(p).Render(&raw); err == nil || raw.Len() != 0 {
		t.Fatal("P2: retained protected payload rendered")
	}
	p.ItemID, p.Title, p.Description = "", "", ""
	raw.Reset()
	if err := c.SidePanelWithSlots(p, c.DetailPanelSlots{Body: []g.Node{g.Text("secret")}, Actions: []g.Node{g.Text("write")}}).Render(&raw); err == nil || raw.Len() != 0 {
		t.Fatal("retained protected slots must refuse before bytes, including export")
	}
	out := html(t, c.SidePanelWithSlots(p, c.DetailPanelSlots{RetryAction: []g.Node{g.Text("retry")}}))
	for _, secret := range []string{"secret", "write", "retry"} {
		if strings.Contains(out, secret) {
			t.Fatalf("P2: refused panel retained %s", secret)
		}
	}
	for _, change := range []func(*c.DetailPanelProps){func(p *c.DetailPanelProps) { p.Label = "" }, func(p *c.DetailPanelProps) { p.CloseLabel = "" }, func(p *c.DetailPanelProps) { p.ReturnHref = "" }, func(p *c.DetailPanelProps) { p.Size = "unknown" }} {
		bad := panelFixture()
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("P1: malformed panel accepted")
		}
	}
}
