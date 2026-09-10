package design_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

func literal(value string) design.ColorValue  { return design.ColorValue{Literal: value} }
func reference(name string) design.ColorValue { return design.ColorValue{Reference: name} }
func blend(first design.ColorValue, percent float64, second design.ColorValue) design.ColorValue {
	return design.ColorValue{Mix: &design.ColorMix{First: first, FirstPercent: percent, Second: second}}
}

func TestColorValuesPreserveAuthoredCSS(t *testing.T) {
	for _, tc := range []struct {
		value design.ColorValue
		want  string
	}{
		{literal("#ffffff"), "#ffffff"},
		{literal("transparent"), "transparent"},
		{reference("--brand"), "var(--brand)"},
		{blend(reference("--brand"), 12, reference("--paper")), "color-mix(in srgb, var(--brand) 12%, var(--paper))"},
		{blend(literal("#ff000080"), 55, literal("transparent")), "color-mix(in srgb, #ff000080 55%, transparent)"},
		{blend(literal("#000"), 12.5, reference("--other")), "color-mix(in srgb, #000 12.5%, var(--other))"},
	} {
		got, err := tc.value.CSS()
		if err != nil || got != tc.want {
			t.Errorf("CSS = %q, %v; want %q", got, err, tc.want)
		}
	}
}

func TestSRGBAValidatesPhysicalChannels(t *testing.T) {
	for _, color := range []design.SRGBA{{}, {1, 1, 1, 1}, {0.2, 0.3, 0.4, 0.05}} {
		if err := color.Validate(); err != nil {
			t.Fatalf("valid sRGB colour refused: %v", err)
		}
	}
	for i := range 4 {
		for _, invalid := range []float64{-0.1, 1.1, math.NaN(), math.Inf(1)} {
			color := design.SRGBA{0, 0, 0, 1}
			color[i] = invalid
			if color.Validate() == nil {
				t.Fatalf("invalid sRGB channel accepted: %v", color)
			}
		}
	}
}

func TestColorResolutionUsesPremultipliedSRGB(t *testing.T) {
	// Independent channel arithmetic, not a second call to the implementation.
	for _, tc := range []struct {
		name  string
		value design.ColorValue
		want  design.SRGBA
	}{
		{"opaque", blend(literal("#f00"), 25, literal("#00f")), design.SRGBA{0.25, 0, 0.75, 1}},
		{"alpha", blend(literal("#ff000088"), 25, literal("#00ff0044")), design.SRGBA{0.4, 0.6, 0, 1.0 / 3}},
		{"overlay", blend(literal("#ff000080"), 55, literal("transparent")), design.SRGBA{1, 0, 0, 128.0 / 255 * 0.55}},
		{"zero alpha", blend(literal("#ff000000"), 50, literal("#0000ff00")), design.SRGBA{}},
		{"zero weight", blend(literal("#f00"), 0, literal("#00f")), design.SRGBA{0, 0, 1, 1}},
		{"full weight", blend(literal("#f00"), 100, literal("#00f")), design.SRGBA{1, 0, 0, 1}},
		{"nested mix", blend(blend(literal("#f00"), 25, literal("#00f")), 50, literal("#0f0")), design.SRGBA{0.125, 0.5, 0.375, 1}},
		{"short alpha", literal("#1a28"), design.SRGBA{17.0 / 255, 170.0 / 255, 34.0 / 255, 136.0 / 255}},
		{"uppercase", literal("#ABCDEF"), design.SRGBA{171.0 / 255, 205.0 / 255, 239.0 / 255, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := design.ResolveColors(nil, []design.ColorToken{{Name: "--result", Value: tc.value}})
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tc.want {
				if math.Abs(got["--result"][i]-want) > 1e-12 {
					t.Fatalf("got %v, want %v", got["--result"], tc.want)
				}
			}
		})
	}
}

func TestColorReferencesRetainIdentityAndDoNotMutateInputs(t *testing.T) {
	base := []design.Token{
		{Name: "--brand", Type: "color", Value: "#112233"},
		{Name: "--focus", Type: "color", Value: "#112233"},
		{Name: "--font", Type: "fontFamily", Value: "sans-serif"},
	}
	roles := []design.ColorToken{
		{Name: "--nested", Value: reference("--button")},
		{Name: "--button", Value: reference("--brand")},
		{Name: "--ring", Value: reference("--focus")},
		{Name: "--overlay", Value: blend(reference("--nested"), 50, literal("transparent"))},
	}
	before, _ := json.Marshal(roles)
	first, err := design.ResolveColors(base, roles)
	if err != nil || len(first) != 6 || first["--button"] != first["--ring"] {
		t.Fatalf("equal values must not collapse identities: %v, %v", first, err)
	}
	base[0].Value = "#ff0000"
	second, err := design.ResolveColors(base, roles)
	if err != nil || second["--button"] == second["--ring"] || second["--nested"] != second["--button"] {
		t.Fatalf("aliases lost authored targets: %v, %v", second, err)
	}
	first["--button"] = design.SRGBA{}
	again, err := design.ResolveColors(base, roles)
	after, _ := json.Marshal(roles)
	if err != nil || !reflect.DeepEqual(second, again) || string(before) != string(after) {
		t.Fatal("resolution mutated inputs or shared an output map")
	}
	var decoded []design.ColorToken
	if err := json.Unmarshal(before, &decoded); err != nil || !reflect.DeepEqual(roles, decoded) {
		t.Fatalf("authored references/mix weights did not round-trip: %v", err)
	}
}

func TestColorSelectionNeedsOnlyItsOwnDependencies(t *testing.T) {
	base := []design.Token{{Name: "--brand", Type: "color", Value: "#ff0000"}}
	shared := blend(reference("--brand"), 50, literal("#0000ff"))
	roles := []design.ColorToken{
		{Name: "--alias", Value: reference("--shared")},
		{Name: "--shared", Value: blend(shared, 25, shared)},
	}
	first, err := design.ResolveColors(base, roles)
	if err != nil || first["--alias"] != (design.SRGBA{0.5, 0, 0.5, 1}) {
		t.Fatalf("shared mix pointers are not cycles and need no unrelated tokens: %v, %v", first, err)
	}
	roles[0], roles[1] = roles[1], roles[0]
	second, err := design.ResolveColors(base, roles)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("input ordering changed reference resolution")
	}
	roles[0].Value = blend(literal("#fff"), 100, reference("--missing"))
	if got, err := design.ResolveColors(base, roles); err == nil || got != nil {
		t.Fatal("the second operand requires a dependency even at zero weight")
	}
	if got, err := design.ResolveColors(nil, nil); err != nil || len(got) != 0 {
		t.Fatal("an empty colour selection requires no ambient palette")
	}
}

func TestColorResolutionRefusesIncompleteOrAmbiguousGraphs(t *testing.T) {
	base := []design.Token{{Name: "--font", Type: "fontFamily", Value: "sans-serif"}}
	cycle := blend(literal("#fff"), 50, literal("#000"))
	cycle.Mix.First = cycle
	for name, values := range map[string][]design.ColorToken{
		"missing":                       {{Name: "--a", Value: reference("--missing")}},
		"wrong type":                    {{Name: "--a", Value: reference("--font")}},
		"duplicate":                     {{Name: "--a", Value: literal("#fff")}, {Name: "--a", Value: literal("#fff")}},
		"base collision":                {{Name: "--font", Value: literal("#fff")}},
		"self cycle":                    {{Name: "--a", Value: reference("--a")}},
		"indirect cycle":                {{Name: "--a", Value: reference("--b")}, {Name: "--b", Value: blend(reference("--a"), 50, literal("#fff"))}},
		"pointer cycle":                 {{Name: "--a", Value: cycle}},
		"zero weight still needs input": {{Name: "--a", Value: blend(reference("--absent"), 0, literal("#fff"))}},
		"empty":                         {{Name: "--a"}},
		"ambiguous":                     {{Name: "--a", Value: design.ColorValue{Literal: "#fff", Reference: "--font"}}},
		"ambiguous mix":                 {{Name: "--a", Value: design.ColorValue{Literal: "#fff", Mix: blend(literal("#fff"), 50, literal("#000")).Mix}}},
		"empty identity":                {{Value: literal("#fff")}},
		"bad identity":                  {{Name: "--bad);color:red", Value: literal("#fff")}},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := design.ResolveColors(base, values)
			if err == nil || got != nil {
				t.Fatalf("invalid graph returned partial success: %v, %v", got, err)
			}
		})
	}
	for _, value := range []design.ColorValue{
		literal("#ggg"), literal("#12"), literal("12px"), literal("red"), literal("rgb(1 2 3)"),
		literal("var(--font)"), reference("font"), reference("--x),red"),
		blend(literal("#fff"), -1, literal("#000")), blend(literal("#fff"), 101, literal("#000")),
		blend(literal("#fff"), math.NaN(), literal("#000")), blend(literal("#fff"), math.Inf(1), literal("#000")),
		cycle,
	} {
		if css, err := value.CSS(); err == nil || css != "" {
			t.Errorf("invalid colour emitted CSS %q, %v", css, err)
		}
		if got, err := design.ResolveColors(nil, []design.ColorToken{{Name: "--a", Value: value}}); err == nil || got != nil {
			t.Errorf("invalid colour resolved: %v, %v", got, err)
		}
	}
	for _, badBase := range [][]design.Token{
		{{Name: "--a", Type: "color", Value: "12px"}},
		{{Name: "--a", Type: "color", Value: "#fff"}, {Name: "--a", Type: "color", Value: "#fff"}},
	} {
		if got, err := design.ResolveColors(badBase, nil); err == nil || got != nil {
			t.Fatalf("invalid base accepted: %v, %v", got, err)
		}
	}
}
