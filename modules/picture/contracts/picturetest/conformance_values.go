package picturetest

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

func invalidValues(t *testing.T, factory func() Decision) {
	template, err := factory().Declare("fixture", SVG)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*contracts.Value){func(v *contracts.Value) { v.Options = nil }, func(v *contracts.Value) { v.Options[0].Option = "unknown" }, func(v *contracts.Value) { v.Options[0].Part = "unknown" }, func(v *contracts.Value) { v.Options = append(v.Options, v.Options[0]) }, func(v *contracts.Value) { v.Colours = nil }, func(v *contracts.Value) { v.Colours[0] = "red" }, func(v *contracts.Value) { v.Background = "" }, func(v *contracts.Value) { v.Background = " #FFFFFF" }} {
		v := Value()
		mutate(&v)
		if body, err := factory().Render(template, v); err == nil || body != "" {
			t.Fatalf("accepted invalid value %+v: %s %v", v, body, err)
		}
	}
}
