package export_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/style"
)

func layoutExample(id string, p c.FlexProps, children ...g.Node) examples.Example {
	return examples.ExampleWithChildren(examples.ExampleInfo{ID: id, ComponentID: "flex"}, p, children, c.Flex)
}

func layoutExport(t *testing.T, captures []examples.Example, extra ...ui.Extra) export.DesignExport {
	t.Helper()
	doc, err := export.ExportWithLayout(design.Default(), captures, extra...)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSourceLayoutPreservesLegacyBytesAndCallerInputs(t *testing.T) {
	captures := examples.Gallery()
	legacy, err := export.Export(design.Default(), captures)
	if err != nil {
		t.Fatal(err)
	}
	// UI gallery revision: native modal roots, responsive rules, shared sections,
	// source property metadata and semantic shape tokens intentionally change v1 content.
	// Cascade-order repair separates the theme and role :root blocks. Compared
	// with the prior CSS owner, all declarations and non-CSS source fields agree.
	// Alert alignment centers its icon, text and dismiss action; only the seven
	// Alert examples change from that baseline, with identical generated CSS.
	// Native checkbox state adds checked/focus/forced-colors CSS and two size
	// utilities. Only four checkbox examples change HTML for sizing and label wrap.
	// Hidden overrides are scoped to components so consumer print styles still work.
	// Checkbox validation adds one optional error property to each of its four
	// source schemas. Gallery HTML, CSS, props, tokens and icons remain identical.
	// Pagination, Breadcrumb and Alert gain optional label properties, so ten
	// source schemas change; HTML, CSS, props, tokens and icons are identical.
	// Table gains an optional Label that makes its scroll wrapper a keyboard-
	// reachable named region, and one new gallery example exercises it. Only the
	// new example's HTML and one new source property are in this delta; tokens,
	// CSS and every other example are byte-identical.
	// Media adds one component and six gallery examples (its five states and a
	// captioned picture); Card's picture now delegates to it with byte-identical
	// output, so no card example moves. Tokens, themes and icons are identical —
	// verified by platformkit-mobile's own token suite, which pins its fixture by
	// provenance and stays green until somebody refreshes it deliberately.
	// Avatar adds one component, six examples and a composed member row; Media gains
	// the Fit property, which was declared and ignored until then. The crop had been
	// baked into the card image geometry, so `fit: "contain"` used to emit
	// `object-cover object-contain` and let the stylesheet decide — the two crops are
	// named classlists now and exactly one is merged per picture. Card's own bytes
	// are unchanged: same classes, same order. Tokens, themes and icons are identical.
	// Avatar gains `label`, the name beside the disc, and with it the rule that the
	// name is audible from exactly one place: labelled and linked is now one anchor
	// around disc and name rather than a labelled disc inside a named one, which said
	// the person's name twice. Avatar's classes move from the gallery-only list to
	// the shell list, because ui/resource composes a person cell with them.
	//
	// This is the digest of the two branches together, measured on the merged tree
	// rather than inherited from either: neither pin above describes a vocabulary
	// that exists after this merge, and a digest copied from one side would be a
	// number that certifies a thing nobody shipped.
	if legacy.SHA256 != "cc4ae73f932192080a7f7f2c270769fc49d615d2d746aadc1243fe94334b5bdd" {
		t.Fatal("v1 baseline changed; investigate rendering and encoding before accepting a migration")
	}
	before, _ := json.Marshal(legacy)
	first := layoutExport(t, captures)
	second := layoutExport(t, captures)
	if !reflect.DeepEqual(first, second) || first.SHA256 == legacy.SHA256 {
		t.Fatal("layout export must be deterministic and distinct from v1")
	}
	after, err := export.Export(design.Default(), captures)
	encoded, _ := json.Marshal(after)
	if err != nil || string(encoded) != string(before) {
		t.Fatal("opt-in capture mutated the existing inputs or v1 export")
	}
	claimed := first.SHA256
	first.SHA256 = ""
	payload, _ := json.Marshal(first)
	hash := sha256.Sum256(payload)
	if claimed != hex.EncodeToString(hash[:]) || first.Schema != "platformkit.design-export.v2" ||
		!reflect.DeepEqual(first.RequiredFeatures, []string{"source-flex-declarations.v1", "source-measurements.v1"}) {
		t.Fatal("v2 did not hash its entire versioned content")
	}
	if !errors.Is(first.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"), export.ErrLayoutUnknown) {
		t.Fatal("gallery contains unmigrated components, not complete portable layout")
	}
}

func TestSourceLayoutUsesResolvedConstructorValues(t *testing.T) {
	for _, tc := range []struct {
		name    string
		props   c.FlexProps
		want    c.FlexLayout
		classes []string
	}{
		{"defaults", c.FlexProps{}, c.FlexLayout{Direction: "row", Gap: "4", Align: "normal", Justify: "normal"}, []string{"flex-row", "gap-4"}},
		{"invalid falls back", c.FlexProps{Direction: "reverse", Gap: "99", Align: "banana", Justify: "evenly"}, c.FlexLayout{Direction: "row", Gap: "4", Align: "normal", Justify: "normal"}, []string{"flex-row", "gap-4"}},
		{"column alias", c.FlexProps{Direction: "column", Gap: "2", Align: "end", Justify: "around", Wrap: true}, c.FlexLayout{Direction: "col", Gap: "2", Align: "end", Justify: "around", Wrap: true}, []string{"flex-col", "gap-2", "items-end", "justify-around", "flex-wrap"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := layoutExport(t, []examples.Example{layoutExample("root", tc.props)})
			d := doc.Examples[0]
			if d.Layout.Kind != "flex" || d.Layout.Flex == nil || *d.Layout.Flex != tc.want {
				t.Fatalf("resolved source layout: %+v", d.Layout)
			}
			for _, class := range tc.classes {
				if !strings.Contains(d.HTML, class) || !strings.Contains(doc.CSS, "."+class+" {") {
					t.Fatalf("declared value lacks constructor/stylesheet evidence: %s", class)
				}
			}
			if err := doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"); err != nil {
				t.Fatalf("required admitted example rejected: %v", err)
			}
		})
	}
}

func TestSourceLayoutRetainsOccurrenceOwnershipAndInterface(t *testing.T) {
	child := examples.ExampleWithChildren(examples.ExampleInfo{ID: "child", ComponentID: "stack"}, c.StackProps{Gap: "2", Align: "center"}, nil, c.Stack)
	root := layoutExample("root", c.FlexProps{}, child.Node)
	doc := layoutExport(t, []examples.Example{root})
	d := doc.Examples[0]
	nested := d.Children[0]
	if nested.Slot != "children" || nested.Span == nil || nested.Description.ID != "child" ||
		nested.Description.Layout.Flex.Direction != "col" || nested.Description.Layout.Flex.Gap != "2" {
		t.Fatal("layout capture lost nested source ownership")
	}
	if err := doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"); err != nil {
		t.Fatal(err)
	}
	changed, err := root.WithProps(json.RawMessage(`{"gap":"8"}`))
	if err != nil {
		t.Fatal(err)
	}
	after := layoutExport(t, []examples.Example{changed})
	if !d.SameInterface(after.Examples[0]) || after.SHA256 == doc.SHA256 ||
		!reflect.DeepEqual(d.Children[0].Description, after.Examples[0].Children[0].Description) {
		t.Fatal("occurrence edit changed its interface/child or failed to invalidate source identity")
	}
}

func TestSourceLayoutUnknownOverridesAndUnobservedChildren(t *testing.T) {
	for name, props := range map[string]c.ComponentProps{
		"class": {Class: "gap-0"}, "attribute": {Attrs: map[string]string{"style": "gap:0"}}, "hidden": {Hidden: true},
	} {
		t.Run(name, func(t *testing.T) {
			doc := layoutExport(t, []examples.Example{layoutExample("root", c.FlexProps{ComponentProps: props})})
			if !errors.Is(doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"), export.ErrLayoutUnknown) || doc.Examples[0].Layout.Flex != nil {
				t.Fatal("escape hatch received a known layout")
			}
		})
	}
	child := layoutExample("child", c.FlexProps{})
	for name, root := range map[string]examples.Example{
		"opaque":     layoutExample("root", c.FlexProps{}, g.Attr("style", "gap:0")),
		"wrapped":    examples.ExampleOf(examples.ExampleInfo{ID: "root", ComponentID: "wrapper"}, c.FlexProps{}, func(p c.FlexProps) g.Node { return h.Section(c.Flex(p)) }),
		"unobserved": examples.ExampleWithChildren(examples.ExampleInfo{ID: "root", ComponentID: "flex"}, c.FlexProps{}, []g.Node{child.Node}, func(p c.FlexProps, _ ...g.Node) g.Node { return c.Flex(p) }),
	} {
		t.Run(name, func(t *testing.T) {
			doc := layoutExport(t, []examples.Example{root})
			if !errors.Is(doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"), export.ErrLayoutUnknown) {
				t.Fatal("unknown ownership was accepted as complete declared layout")
			}
			if name == "unobserved" && (doc.Examples[0].Children[0].Span != nil || doc.Examples[0].Children[0].Description.Layout.Flex != nil) {
				t.Fatal("unobserved child gained a rendered layout")
			}
		})
	}
	for _, media := range []bool{false, true} {
		sheet := css.NewSheet()
		write := func(s *css.Sheet) { s.Select(".flex", css.Decl("flex-direction", css.Literal("column"))) }
		if media {
			sheet.Media("(min-width: 4000px)", write)
		} else {
			write(sheet)
		}
		doc := layoutExport(t, []examples.Example{layoutExample("root", c.FlexProps{}, child.Node)}, ui.Extra{Sheets: []*css.Sheet{sheet}})
		if !errors.Is(doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"), export.ErrLayoutUnknown) ||
			doc.Examples[0].Layout.Flex != nil || doc.Examples[0].Children[0].Description.Layout.Flex != nil {
			t.Fatal("consumer stylesheet retained an invalidated claim")
		}
	}
	doc := layoutExport(t, []examples.Example{child}, ui.Extra{Lists: []style.ClassList{style.New().Gap(style.S4)}, Sheets: []*css.Sheet{nil, css.NewSheet()}})
	if err := doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"); err != nil {
		t.Fatalf("identical owned utility/empty sheet is not an override: %v", err)
	}
}

func TestSourceLayoutRefusesUnknownVersionsFeaturesAndMalformedDeclarations(t *testing.T) {
	base := layoutExport(t, []examples.Example{layoutExample("root", c.FlexProps{})})
	for name, change := range map[string]func(*export.DesignExport){
		"version":               func(d *export.DesignExport) { d.Schema = "platformkit.design-export.v3" },
		"missing requirement":   func(d *export.DesignExport) { d.RequiredFeatures = nil },
		"unknown requirement":   func(d *export.DesignExport) { d.RequiredFeatures = append(d.RequiredFeatures, "future.v1") },
		"duplicate requirement": func(d *export.DesignExport) { d.RequiredFeatures = append(d.RequiredFeatures, d.RequiredFeatures[0]) },
		"future kind":           func(d *export.DesignExport) { d.Examples[0].Layout.Kind = "future" },
		"invalid direction":     func(d *export.DesignExport) { d.Examples[0].Layout.Flex.Direction = "reverse" },
		"invalid gap":           func(d *export.DesignExport) { d.Examples[0].Layout.Flex.Gap = "auto" },
		"missing flex":          func(d *export.DesignExport) { d.Examples[0].Layout.Flex = nil },
		"conflicting reason":    func(d *export.DesignExport) { d.Examples[0].Layout.Reason = "unowned" },
	} {
		t.Run(name, func(t *testing.T) {
			bytes, _ := json.Marshal(base)
			var doc export.DesignExport
			if err := json.Unmarshal(bytes, &doc); err != nil {
				t.Fatal(err)
			}
			change(&doc)
			before, _ := json.Marshal(doc)
			if err := doc.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1", "future.v1"); !errors.Is(err, export.ErrLayoutUnsupported) {
				t.Fatalf("unsupported contract accepted: %v", err)
			}
			after, _ := json.Marshal(doc)
			if string(before) != string(after) {
				t.Fatal("validation mutated caller-owned data")
			}
		})
	}
	if !errors.Is(base.CheckLayoutContract(), export.ErrLayoutUnsupported) {
		t.Fatal("consumer lacking the required feature was accepted")
	}
	base.Examples[0].Layout = nil
	if !errors.Is(base.CheckLayoutContract("source-flex-declarations.v1", "source-measurements.v1"), export.ErrLayoutUnknown) {
		t.Fatal("missing layout was interpreted as a default")
	}
}
