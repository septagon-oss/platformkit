package style_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

func TestShadowValuesOwnOrderedLayersAndPreserveCSS(t *testing.T) {
	values, err := style.ShadowValues()
	if err != nil || len(values) != 8 {
		t.Fatalf("shadow inventory: %d, %v", len(values), err)
	}
	wantCSS := map[string]string{
		"sm":    "0 1px 2px 0 rgb(0 0 0 / 0.05)",
		"base":  "0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)",
		"md":    "0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)",
		"lg":    "0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)",
		"xl":    "0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)",
		"2xl":   "0 25px 50px -12px rgb(0 0 0 / 0.25)",
		"inner": "inset 0 2px 4px 0 rgb(0 0 0 / 0.05)",
		"none":  "0 0 #0000",
	}
	for _, value := range values {
		if err := value.Validate(); err != nil {
			t.Fatal(err)
		}
		class := "shadow-" + value.Key
		if value.Key == "base" {
			class = "shadow"
		}
		sheet, err := style.Rules(class)
		if err != nil || !strings.Contains(sheet.CSS(), "box-shadow: "+wantCSS[value.Key]+";") {
			t.Fatalf("%s changed legacy CSS: %v", class, err)
		}
		if value.Key == "base" {
			if len(value.Layers) != 2 || value.Layers[0].Blur.Value != "3" || value.Layers[1].Spread.Value != "-1" || value.Layers[1].Color != (design.SRGBA{0, 0, 0, 0.1}) {
				t.Fatalf("base shadow order, signed spread or colour lost: %+v", value)
			}
		}
		if value.Key == "none" && (len(value.Layers) != 1 || value.Layers[0].Blur != (style.Scalar{}) || value.Layers[0].Spread != (style.Scalar{}) || value.Layers[0].Color != (design.SRGBA{})) {
			t.Fatal("shadow-none must retain its authored transparent layer and omitted optional lengths")
		}
		if value.Key == "inner" && !value.Layers[0].Inset {
			t.Fatal("inset shadow flattened into an outer shadow")
		}
	}
	before, _ := json.Marshal(values)
	values[0].Layers[0].OffsetX.Value = "100"
	again, err := style.ShadowValues()
	after, _ := json.Marshal(again)
	if err != nil || string(before) != string(after) {
		t.Fatal("caller mutation changed the owning shadow table")
	}
}

func TestShadowValuesRefuseInvalidLengthsAndColors(t *testing.T) {
	valid := style.ShadowLayer{OffsetX: style.Scalar{Value: "-1", Unit: "em"}, OffsetY: style.Scalar{Value: "0", Unit: "px"},
		Blur: style.Scalar{Value: "2", Unit: "rem"}, Spread: style.Scalar{Value: "-1", Unit: "px"}, Color: design.SRGBA{0.2, 0.3, 0.4, 0.5}, Inset: true}
	if err := (style.ShadowValue{Key: "base", Layers: []style.ShadowLayer{valid}}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*style.ShadowLayer){
		func(v *style.ShadowLayer) { v.Blur.Value = "-1" },
		func(v *style.ShadowLayer) { v.OffsetX.Unit = "%" },
		func(v *style.ShadowLayer) { v.Blur = style.Scalar{} },
		func(v *style.ShadowLayer) { v.Color[3] = 1.1 },
		func(v *style.ShadowLayer) { v.Color[0] = math.NaN() },
		func(v *style.ShadowLayer) { v.Spread.Value = "1e999" },
	} {
		layer := valid
		mutate(&layer)
		if (style.ShadowValue{Key: "base", Layers: []style.ShadowLayer{layer}}).Validate() == nil {
			t.Errorf("invalid shadow accepted: %+v", layer)
		}
	}
	for _, value := range []style.ShadowValue{{Key: "base"}, {Key: "future", Layers: []style.ShadowLayer{valid}}} {
		if value.Validate() == nil {
			t.Fatal("unknown or empty shadow accepted")
		}
	}
}

func TestTimingValuesKeepCurvesAndTransitionReferences(t *testing.T) {
	easings, err := style.EasingValues()
	if err != nil || len(easings) != 4 {
		t.Fatalf("easing inventory: %v", err)
	}
	for _, easing := range easings {
		if err := easing.Validate(); err != nil {
			t.Fatal(err)
		}
		if easing.Key == "linear" && (easing.Keyword != "linear" || easing.CubicBezier != nil) {
			t.Fatal("linear keyword changed into an inferred curve")
		}
		if easing.Key == "in-out" && (easing.CubicBezier == nil || *easing.CubicBezier != ([4]float64{0.4, 0, 0.2, 1})) {
			t.Fatal("cubic control points changed")
		}
	}
	transitions, err := style.TransitionValues()
	if err != nil || len(transitions) != 6 {
		t.Fatalf("transition inventory: %v", err)
	}
	for _, transition := range transitions {
		if err := transition.Validate(); err != nil {
			t.Fatal(err)
		}
		sheet, err := style.Rules("transition-" + transition.Key)
		if err != nil || !strings.Contains(sheet.CSS(), "transition-property: "+strings.Join(transition.Properties, ", ")+";") {
			t.Fatalf("transition property order differs from CSS: %v", err)
		}
		if transition.Key == "none" {
			if transition.Duration != "" || transition.Easing != "" || strings.Contains(sheet.CSS(), "transition-duration") {
				t.Fatal("transition-none must not invent timing declarations")
			}
		} else if transition.Duration != "150" || transition.Easing != "in-out" || !strings.Contains(sheet.CSS(), "transition-duration: 150ms;") || !strings.Contains(sheet.CSS(), "transition-timing-function: cubic-bezier(0.4, 0, 0.2, 1);") {
			t.Fatal("transition defaults lost owning scale references")
		}
	}
	before, _ := json.Marshal([]any{easings, transitions})
	for _, value := range easings {
		if value.CubicBezier != nil {
			value.CubicBezier[0] = 0.99
		}
	}
	transitions[0].Properties[0] = "changed"
	easings, _ = style.EasingValues()
	transitions, _ = style.TransitionValues()
	after, _ := json.Marshal([]any{easings, transitions})
	if string(before) != string(after) {
		t.Fatal("caller mutation changed timing or property owners")
	}
}

func TestTimingValuesAllowOvershootButRefuseInvalidReferences(t *testing.T) {
	curve := style.EasingValue{Key: "in-out", CubicBezier: &[4]float64{0.3, -0.5, 0.6, 1.5}}
	if err := curve.Validate(); err != nil {
		t.Fatalf("Y overshoot is valid: %v", err)
	}
	for _, value := range []style.EasingValue{
		{Key: "future", Keyword: "linear"}, {Key: "linear"}, {Key: "linear", Keyword: "future"},
		{Key: "linear", Keyword: "linear", CubicBezier: &[4]float64{}},
		{Key: "in", CubicBezier: &[4]float64{-0.1, 0, 0.5, 1}},
		{Key: "out", CubicBezier: &[4]float64{0.1, 0, 1.1, 1}},
		{Key: "in-out", CubicBezier: &[4]float64{0.1, math.Inf(1), 0.5, 1}},
	} {
		if value.Validate() == nil {
			t.Fatal("invalid timing curve accepted")
		}
	}
	for _, value := range []style.TransitionValue{
		{Key: "colors", Properties: []string{"color"}, Duration: "future", Easing: "in-out"},
		{Key: "colors", Properties: []string{"color"}, Duration: "150", Easing: "future"},
		{Key: "colors", Properties: []string{"color", "none"}, Duration: "150", Easing: "in-out"},
		{Key: "colors", Properties: []string{"color;color:red"}, Duration: "150", Easing: "in-out"},
		{Key: "none", Properties: []string{"none"}, Duration: "150", Easing: "in-out"},
		{Key: "colors"},
	} {
		if value.Validate() == nil {
			t.Fatal("invalid transition or reference accepted")
		}
	}
}
