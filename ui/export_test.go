package ui_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/icon"
	g "maragu.dev/gomponents"
)

func TestDesignExportUsesCurrentRenderingAndAssets(t *testing.T) {
	theme := design.Default()
	theme.Light.AccentDefault = "#abcdef"
	examples := c.Gallery()
	doc, err := ui.Export(theme, examples)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "platformkit.design-export.v1" || doc.FontPolicy != "system-fallback-stacks" {
		t.Fatalf("unexpected export boundary: %s / %s", doc.Schema, doc.FontPolicy)
	}
	for _, path := range []string{"../LICENSE", "../NOTICE"} {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc.Notices, string(text)) {
			t.Fatalf("export attribution is stale relative to %s", path)
		}
	}
	wantCSS := ui.Compose(theme, ui.Extra{Lists: c.ClassLists()})
	if doc.CSS != string(wantCSS.Body) || !strings.Contains(doc.CSS, "#abcdef") {
		t.Fatal("export did not use the current themed component stylesheet")
	}
	if len(doc.Themes) != 2 || doc.Themes[0].Mode != "light" || doc.Themes[1].Mode != "dark" {
		t.Fatalf("theme mode selectors do not match CSS: %+v", doc.Themes)
	}
	if len(doc.Examples) != len(examples) || len(doc.Icons) != len(icon.Names()) {
		t.Fatal("export omitted gallery entries or canonical glyphs")
	}
	for _, example := range examples {
		description, err := example.Describe()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(doc.Examples, func(d c.ExampleDescription) bool { return d.ID == example.ID })
		if index < 0 || doc.Examples[index].HTML != description.HTML {
			t.Fatalf("exported rendering differs for %s", example.ID)
		}
	}
	for _, asset := range doc.Icons {
		glyph, known := icon.Resolve(asset.Name)
		if !known || !strings.Contains(asset.SVG, glyph.Body) || !strings.Contains(asset.SVG, `viewBox="0 0 256 256"`) {
			t.Fatalf("export invented or changed glyph %s", asset.Name)
		}
		hash := sha256.Sum256([]byte(asset.SVG))
		if asset.SHA256 != hex.EncodeToString(hash[:]) || asset.Source != glyph.Source || asset.License != glyph.License {
			t.Fatalf("incomplete asset provenance: %+v", asset)
		}
	}
}

func TestDesignExportIsDeterministicAndContentAddressed(t *testing.T) {
	examples := c.Gallery()
	first, err := ui.Export(design.Default(), examples)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(examples)
	second, err := ui.Export(design.Default(), examples)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Fatal("source ordering changed canonical export")
	}
	claimed := first.SHA256
	first.SHA256 = ""
	payload, _ := json.Marshal(first)
	hash := sha256.Sum256(payload)
	if claimed != hex.EncodeToString(hash[:]) {
		t.Fatal("digest is not the canonical payload without sha256")
	}
	theme := design.Default()
	theme.Dark.AccentDefault = "#123456"
	changed, err := ui.Export(theme, examples)
	if err != nil || changed.SHA256 == claimed {
		t.Fatalf("changed token did not invalidate artifact: %v", err)
	}
	index := slices.IndexFunc(examples, func(e c.Example) bool { return e.ID == "pk-ui.component.button/primary" })
	if index < 0 {
		t.Fatal("stable button identity missing")
	}
	examples[index], err = examples[index].WithProps(json.RawMessage(`{"label":"A changed label"}`))
	if err != nil {
		t.Fatal(err)
	}
	changed, err = ui.Export(design.Default(), examples)
	if err != nil || changed.SHA256 == claimed {
		t.Fatalf("changed props did not invalidate artifact: %v", err)
	}
}

func TestDesignExportRejectsAmbiguousIdentityAndRenderFailures(t *testing.T) {
	info := c.ExampleInfo{ID: "button/one", ComponentID: "button", Name: "Button"}
	button := c.ExampleOf(info, c.ButtonProps{Label: "Save"}, c.Button)
	otherInfo := info
	otherInfo.ID = "button/two"
	for _, examples := range [][]c.Example{
		{button, button},
		{c.ExampleOf(c.ExampleInfo{}, c.ButtonProps{}, c.Button)},
		{button, c.ExampleOf(otherInfo, c.TextProps{Content: "Different contract"}, c.Text)},
		{c.ExamplePreview(info, failedExportNode{}, "Render failure must be visible")},
	} {
		if _, err := ui.Export(design.Default(), examples); err == nil {
			t.Fatal("accepted duplicate/missing identity, conflicting props contract or render failure")
		}
	}
}

type failedExportNode struct{}

func (failedExportNode) Render(io.Writer) error { return errors.New("fixture render failed") }

func TestDesignExportKeepsPreviewAndGoSlotSupportExplicit(t *testing.T) {
	info := c.ExampleInfo{ID: "button/icon", ComponentID: "button"}
	button := c.ExampleWithSlots(info, c.ButtonProps{Label: "Save"}, c.ButtonSlots{IconEnd: []g.Node{g.Text("Icon")}}, c.ButtonWithSlots)
	previewInfo := c.ExampleInfo{ID: "helper/preview", ComponentID: "helper"}
	preview := c.ExamplePreview(previewInfo, g.Text("Preview"), "No typed property contract")
	doc, err := ui.Export(design.Default(), []c.Example{button, preview})
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Examples[0].PropsEditable || doc.Examples[1].PropsEditable || doc.Examples[1].Reason == "" {
		t.Fatal("export concealed preview-only support")
	}
	for _, slot := range doc.Examples[0].Slots {
		if slot.Supported && !slot.TrustedOnly {
			t.Fatal("trusted Go replacement advertised as portable/native support")
		}
	}
}
