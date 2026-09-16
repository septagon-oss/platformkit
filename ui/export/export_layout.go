package export

import (
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/style"
)

var (
	ErrLayoutUnknown     = errors.New("source layout is unknown")
	ErrLayoutUnsupported = errors.New("source layout contract is unsupported")
)

// ExportWithLayout opts into v2 root declarations through the existing capture
// and stylesheet composition path. It makes no native/editability guarantee.
func ExportWithLayout(theme design.Pair, captures []examples.Example, extra ...ui.Extra) (DesignExport, error) {
	return export(theme, captures, true, extra...)
}

func invalidateLayout(captures []examples.ExampleDescription) {
	var invalidate func(*examples.ExampleDescription)
	invalidate = func(d *examples.ExampleDescription) {
		d.Layout = &components.LayoutDescription{Kind: "unknown", Reason: "consumer stylesheet may override root declarations"}
		for i := range d.Children {
			invalidate(&d.Children[i].Description)
		}
	}
	for i := range captures {
		invalidate(&captures[i])
	}
}

// CheckLayoutContract checks source-generated Go values against this revision's
// declaration contract and the caller's understood features. It neither decodes JSON
// nor verifies a hash, CSS equivalence, native capabilities or write authority.
// Unknown source is distinct from an unsupported version/feature/value. Callers
// must separately prove the requested projection before performing effects.
func (d DesignExport) CheckLayoutContract(supported ...string) error {
	const layoutFeature, measureFeature = "source-flex-declarations.v1", "source-measurements.v1"
	if d.Schema != "platformkit.design-export.v2" || !slices.Contains(d.RequiredFeatures, layoutFeature) {
		return fmt.Errorf("%w: schema or required features", ErrLayoutUnsupported)
	}
	required := map[string]bool{}
	for _, feature := range d.RequiredFeatures {
		if required[feature] || (feature != layoutFeature && feature != measureFeature) || !slices.Contains(supported, feature) {
			return fmt.Errorf("%w: required feature %q", ErrLayoutUnsupported, feature)
		}
		required[feature] = true
	}
	measurements := map[string]bool{}
	for _, measure := range d.Measurements {
		key := measure.Scale + "/" + measure.Key
		if !required[measureFeature] || measurements[key] || measure.Validate() != nil {
			return fmt.Errorf("%w: measurement %q", ErrLayoutUnsupported, key)
		}
		measurements[key] = true
	}
	var check func(examples.ExampleDescription, []string) error
	check = func(example examples.ExampleDescription, parent []string) error {
		path := append(slices.Clone(parent), example.ID)
		layout := example.Layout
		if layout == nil || (layout.Kind == "unknown" && layout.Flex == nil && layout.Reason != "") {
			return fmt.Errorf("%w at %q", ErrLayoutUnknown, path)
		}
		if layout.Kind != "flex" || layout.Reason != "" || layout.Flex == nil {
			return fmt.Errorf("%w at %q: declaration shape", ErrLayoutUnsupported, path)
		}
		flow := layout.Flex
		if !slices.Contains([]style.FlexDir{style.FlexRow, style.FlexCol}, flow.Direction) ||
			!slices.Contains([]style.Items{"normal", style.ItemsStart, style.ItemsCenter, style.ItemsEnd, style.ItemsStretch}, flow.Align) ||
			!slices.Contains([]style.Justify{"normal", style.JustifyStart, style.JustifyCenter, style.JustifyEnd, style.JustifyBetween, style.JustifyAround}, flow.Justify) ||
			!slices.Contains(style.AllSpacings(), flow.Gap) || flow.Gap == style.SAuto || flow.Gap == style.SFull {
			return fmt.Errorf("%w at %q: declaration values", ErrLayoutUnsupported, path)
		}
		if len(example.OpaqueSlots) != 0 {
			return fmt.Errorf("%w at %q: opaque slots", ErrLayoutUnknown, path)
		}
		if required[measureFeature] && !measurements["spacing/"+string(flow.Gap)] {
			return fmt.Errorf("%w at %q: gap measurement %q", ErrLayoutUnknown, path, flow.Gap)
		}
		for _, child := range example.Children {
			if child.Span == nil || child.Slot == "" {
				return fmt.Errorf("%w at %q: child %q ownership", ErrLayoutUnknown, path, child.Description.ID)
			}
			if err := check(child.Description, path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, example := range d.Examples {
		if err := check(example, nil); err != nil {
			return err
		}
	}
	return nil
}
