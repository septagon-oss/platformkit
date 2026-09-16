package export_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/icon"
)

func TestDesignExportUsesCurrentRenderingAndAssets(t *testing.T) {
	theme := design.Default()
	theme.Light.AccentDefault = "#abcdef"
	captures := examples.Gallery()
	doc, err := export.Export(theme, captures)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "platformkit.design-export.v1" || doc.FontPolicy != "system-fallback-stacks" {
		t.Fatalf("unexpected export boundary: %s / %s", doc.Schema, doc.FontPolicy)
	}
	for _, path := range []string{"../../LICENSE", "../../NOTICE"} {
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
	if len(doc.Examples) != len(captures) || len(doc.Icons) != len(icon.Names()) {
		t.Fatal("export omitted gallery entries or canonical glyphs")
	}
	for _, example := range captures {
		description, err := example.Describe()
		if err != nil {
			t.Fatal(err)
		}
		index := slices.IndexFunc(doc.Examples, func(d examples.ExampleDescription) bool { return d.ID == example.ID })
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
	captures := examples.Gallery()
	first, err := export.Export(design.Default(), captures)
	if err != nil {
		t.Fatal(err)
	}
	slices.Reverse(captures)
	second, err := export.Export(design.Default(), captures)
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
	changed, err := export.Export(theme, captures)
	if err != nil || changed.SHA256 == claimed {
		t.Fatalf("changed token did not invalidate artifact: %v", err)
	}
	index := slices.IndexFunc(captures, func(e examples.Example) bool { return e.ID == "pk-ui.component.button/primary" })
	if index < 0 {
		t.Fatal("stable button identity missing")
	}
	captures[index], err = captures[index].WithProps(json.RawMessage(`{"label":"A changed label"}`))
	if err != nil {
		t.Fatal(err)
	}
	changed, err = export.Export(design.Default(), captures)
	if err != nil || changed.SHA256 == claimed {
		t.Fatalf("changed props did not invalidate artifact: %v", err)
	}
}

func TestTypographyConfigurationReachesRuntimeAndExportWithoutChangingComponents(t *testing.T) {
	t.Parallel()
	theme := design.Default()
	captures := examples.Gallery()
	before, err := export.Export(theme, captures)
	if err != nil {
		t.Fatal(err)
	}
	theme.Light.Typography.Display = `"Customer Display", serif`
	theme.Dark.Typography.Display = theme.Light.Typography.Display
	after, err := export.Export(theme, captures)
	if err != nil {
		t.Fatal(err)
	}
	if after.SHA256 == before.SHA256 || after.CSS == before.CSS || ui.Compose(theme).Fingerprint == ui.Compose(design.Default()).Fingerprint {
		t.Fatal("typography must invalidate both source and stylesheet identities")
	}
	if !reflect.DeepEqual(after.Examples, before.Examples) || !reflect.DeepEqual(after.Icons, before.Icons) || after.FontPolicy != before.FontPolicy {
		t.Fatal("typography configuration changed component contracts, assets or font-delivery policy")
	}
	for _, palette := range after.Themes {
		index := slices.IndexFunc(palette.Tokens, func(token design.Token) bool { return token.Name == "--pk-font-display" })
		if index < 0 || palette.Tokens[index].Value != `"Customer Display", serif` ||
			!strings.Contains(after.CSS, `--pk-font-display: "Customer Display", serif;`) {
			t.Fatalf("exported typography is missing or disagrees with the source stylesheet: %+v", palette)
		}
	}
}

func TestDesignExportRejectsAmbiguousIdentityAndRenderFailures(t *testing.T) {
	info := examples.ExampleInfo{ID: "button/one", ComponentID: "button", Name: "Button"}
	button := examples.ExampleOf(info, c.ButtonProps{Label: "Save"}, c.Button)
	otherInfo := info
	otherInfo.ID = "button/two"
	for _, captures := range [][]examples.Example{
		{button, button},
		{examples.ExampleOf(examples.ExampleInfo{}, c.ButtonProps{}, c.Button)},
		{button, examples.ExampleOf(otherInfo, c.TextProps{Content: "Different contract"}, c.Text)},
		{examples.ExamplePreview(info, failedExportNode{}, "Render failure must be visible")},
	} {
		if _, err := export.Export(design.Default(), captures); err == nil {
			t.Fatal("accepted duplicate/missing identity, conflicting props contract or render failure")
		}
	}
}

type failedExportNode struct{}

func (failedExportNode) Render(io.Writer) error { return errors.New("fixture render failed") }

func TestDesignExportKeepsPreviewAndGoSlotSupportExplicit(t *testing.T) {
	info := examples.ExampleInfo{ID: "button/icon", ComponentID: "button"}
	button := examples.ExampleWithSlots(info, c.ButtonProps{Label: "Save"}, c.ButtonSlots{IconEnd: []g.Node{g.Text("Icon")}}, c.ButtonWithSlots)
	previewInfo := examples.ExampleInfo{ID: "helper/preview", ComponentID: "helper"}
	preview := examples.ExamplePreview(previewInfo, g.Text("Preview"), "No typed property contract")
	doc, err := export.Export(design.Default(), []examples.Example{button, preview})
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

func slotContractExample[S any](info examples.ExampleInfo, label string, slots S) examples.Example {
	return examples.ExampleWithSlots(info, c.ButtonProps{Label: label}, slots, func(p c.ButtonProps, _ S) g.Node {
		return g.Text(p.Label)
	})
}

func TestDesignExportRejectsConflictingSlotAndEditabilityContracts(t *testing.T) {
	info := examples.ExampleInfo{ID: "owner/first", ComponentID: "owner"}
	base := slotContractExample(info, "First", struct{ Body g.Node }{g.Text("Body")})
	other := examples.ExampleInfo{ID: "owner/second", ComponentID: "owner"}
	cases := map[string]examples.Example{
		"missing":  slotContractExample(other, "Second", struct{}{}),
		"extra":    slotContractExample(other, "Second", struct{ Body, Footer g.Node }{}),
		"renamed":  slotContractExample(other, "Second", struct{ Content g.Node }{}),
		"multiple": slotContractExample(other, "Second", struct{ Body []g.Node }{}),
		"callback": slotContractExample(other, "Second", struct{ Body func() g.Node }{}),
		"preview":  examples.ExamplePreview(other, g.Text("Preview"), "No typed contract"),
	}
	for name, example := range cases {
		t.Run(name, func(t *testing.T) {
			for _, captures := range [][]examples.Example{{base, example}, {example, base}} {
				doc, err := export.Export(design.Default(), captures)
				if err == nil || !reflect.DeepEqual(doc, export.DesignExport{}) {
					t.Fatalf("conflicting interface must fail without partial export: error=%v, examples=%d", err, len(doc.Examples))
				}
			}
		})
	}
	callback := slotContractExample(info, "First", struct{ Body func() g.Node }{})
	changed := slotContractExample(other, "Second", struct{ Body func(string) g.Node }{})
	if doc, err := export.Export(design.Default(), []examples.Example{callback, changed}); err == nil || !reflect.DeepEqual(doc, export.DesignExport{}) {
		t.Fatal("accepted conflicting opaque callback types or returned a partial export")
	}
}

func TestDesignExportSlotContractsIgnoreDeclarationOrderAndExampleValues(t *testing.T) {
	type ordered struct {
		Body   g.Node
		Footer []g.Node
	}
	type reversed struct {
		Footer []g.Node
		Body   g.Node
	}
	first := examples.ExampleInfo{ID: "owner/first", ComponentID: "owner", Name: "First", Group: "One"}
	second := examples.ExampleInfo{ID: "owner/second", ComponentID: "owner", Name: "Second", Group: "Two"}
	captures := []examples.Example{
		slotContractExample(first, "First label", ordered{g.Text("Body"), nil}),
		slotContractExample(second, "Second label", reversed{[]g.Node{g.Text("Footer")}, g.Text("Changed")}),
	}
	doc, err := export.Export(design.Default(), captures)
	if err != nil || len(doc.Examples) != 2 {
		t.Fatalf("compatible interfaces rejected: %v", err)
	}
	if string(doc.Examples[0].Props) == string(doc.Examples[1].Props) || doc.Examples[0].HTML == doc.Examples[1].HTML {
		t.Fatal("export discarded distinct example values")
	}
}

func TestDesignExportValidatesNestedContractsWithoutGlobalChildIDs(t *testing.T) {
	childInfo := examples.ExampleInfo{ID: "action", ComponentID: "button"}
	button := examples.ExampleOf(childInfo, c.ButtonProps{Label: "Save"}, c.Button)
	parent := func(id string, child examples.Example, hide bool) examples.Example {
		return examples.ExampleWithChildren(examples.ExampleInfo{ID: id, ComponentID: "form"}, c.FormProps{}, []g.Node{child.Node},
			func(p c.FormProps, nodes ...g.Node) g.Node {
				if hide {
					return c.Form(p)
				}
				return c.Form(p, nodes...)
			})
	}
	first := parent("first", button, false)
	if _, err := export.Export(design.Default(), []examples.Example{first, parent("second", button, false)}); err != nil {
		t.Fatalf("the same local child identity in different owners is valid: %v", err)
	}
	conflicting := examples.ExampleOf(childInfo, c.TextProps{Content: "Different interface"}, c.Text)
	for _, hide := range []bool{false, true} {
		doc, err := export.Export(design.Default(), []examples.Example{first, parent("second", conflicting, hide)})
		if err == nil || !reflect.DeepEqual(doc, export.DesignExport{}) {
			t.Fatalf("nested interface conflict escaped validation (unobserved=%v): %v", hide, err)
		}
	}
}
