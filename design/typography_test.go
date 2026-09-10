package design_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
)

func TestFontWeightUsesExactSharedNumericDomain(t *testing.T) {
	t.Parallel()
	for _, weight := range []json.Number{"1", "1e3", "1000.000", "650.5", "1.00000000000000001", "999.99999999999999999"} {
		if err := design.ValidateFontWeight(weight); err != nil {
			t.Fatalf("valid numeric weight %s: %v", weight, err)
		}
	}
	for _, weight := range []json.Number{"", "null", "NaN", "1e999", "+400", "0400", "1001", "0", "1000.00000000000000001", "0.99999999999999999999"} {
		if err := design.ValidateFontWeight(weight); err == nil {
			t.Fatalf("invalid numeric weight %s admitted", weight)
		}
	}
}

func TestFontFamiliesPreserveOrderedMeaningAndExistingIdentities(t *testing.T) {
	t.Parallel()
	want := []design.FontFamilyToken{
		{Name: "--pk-font-display", Families: []design.FontFamily{
			{Name: "Iowan Old Style"}, {Name: "Palatino Linotype"}, {Name: "Palatino"}, {Name: "Georgia"}, {Name: "serif", Generic: true},
		}},
		{Name: "--pk-font-body", Families: []design.FontFamily{
			{Name: "IBM Plex Sans"}, {Name: "Aptos"}, {Name: "Helvetica Neue"}, {Name: "sans-serif", Generic: true},
		}},
		{Name: "--pk-font-mono", Families: []design.FontFamily{
			{Name: "IBM Plex Mono"}, {Name: "SFMono-Regular"}, {Name: "Consolas"}, {Name: "monospace", Generic: true},
		}},
	}
	for _, theme := range design.Default().Both() {
		got, err := theme.FontFamilies()
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("%s font families = %#v, %v", theme.Name, got, err)
		}
		got[0].Families[0].Name = "consumer edit"
		got[1].Families = nil
		again, err := theme.FontFamilies()
		if err != nil || !reflect.DeepEqual(again, want) {
			t.Fatal("projection retained caller-owned output")
		}
	}
}

func TestFontFamilyProjectionDistinguishesCSSNamesFromKeywords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		css  string
		want []design.FontFamily
	}{
		{`"Example, Sans", 'serif', SeRiF`, []design.FontFamily{{Name: "Example, Sans"}, {Name: "serif"}, {Name: "serif", Generic: true}}},
		{"\tExample  \tSans\n, sans-serif ", []design.FontFamily{{Name: "Example Sans"}, {Name: "sans-serif", Generic: true}}},
		{`"  Spaced  Name  ", '21st Century', 中文, Café`, []design.FontFamily{{Name: "  Spaced  Name  "}, {Name: "21st Century"}, {Name: "中文"}, {Name: "Café"}}},
		{`"inherit", "default", "menu", system-ui`, []design.FontFamily{{Name: "inherit"}, {Name: "default"}, {Name: "menu"}, {Name: "system-ui", Generic: true}}},
		{`--Custom, -Custom, Custom_2, "Name|Face"`, []design.FontFamily{{Name: "--Custom"}, {Name: "-Custom"}, {Name: "Custom_2"}, {Name: "Name|Face"}}},
		{`"Name With NBSP", Name With NBSP`, []design.FontFamily{{Name: "Name With NBSP"}, {Name: "Name With NBSP"}}},
		{`ui-serİf, UI-SERIF`, []design.FontFamily{{Name: "ui-serİf"}, {Name: "ui-serif", Generic: true}}},
		{`serif, sans-serif, monospace, cursive, fantasy, system-ui, ui-serif, ui-sans-serif, ui-monospace, ui-rounded, math`, []design.FontFamily{
			{Name: "serif", Generic: true}, {Name: "sans-serif", Generic: true}, {Name: "monospace", Generic: true},
			{Name: "cursive", Generic: true}, {Name: "fantasy", Generic: true}, {Name: "system-ui", Generic: true},
			{Name: "ui-serif", Generic: true}, {Name: "ui-sans-serif", Generic: true}, {Name: "ui-monospace", Generic: true},
			{Name: "ui-rounded", Generic: true}, {Name: "math", Generic: true},
		}},
	} {
		t.Run(tc.css, func(t *testing.T) {
			theme := design.Light()
			theme.Typography.Body = tc.css
			before := theme
			css := design.CSS(theme, design.Dark()).CSS()
			got, err := theme.FontFamilies()
			if err != nil || !reflect.DeepEqual(got[1].Families, tc.want) {
				t.Fatalf("projection = %#v, %v; want %#v", got, err, tc.want)
			}
			if theme != before || design.CSS(theme, design.Dark()).CSS() != css {
				t.Fatal("projection changed the comparable theme or its existing CSS")
			}
		})
	}
}

func TestFontFamilyProjectionRefusesUnsupportedMeaningWithoutPartialOutput(t *testing.T) {
	t.Parallel()
	for _, css := range []string{
		" ", ",Arial", "Arial,", "Arial,,serif", `"unterminated`, `""`, `''`,
		`"Lucida" Grande`, `"A""B"`, `Ahem!`, `Red/Black`, `Hawaii 5-0`, "-2face", "-",
		"inherit", "INITIAL", "unset", "revert", "revert-layer", "default", "menu", "status-bar",
		"serif Name", "Name sans-serif", "Name inherit", "var(--font)", "generic(kai)",
		"emoji", "fangsong", `Escaped\ Name`, `"Escaped\22 Name"`, "Arial/*comment*/",
		"\"New\nLine\"", "\"New\rLine\"", "\"New\fLine\"", "\"Nul\x00Name\"", "\xff",
	} {
		t.Run(css, func(t *testing.T) {
			theme := design.Light()
			theme.Typography.Mono = css // A failure after valid roles must return no prefix.
			before := theme
			got, err := theme.FontFamilies()
			if err == nil || got != nil || !strings.Contains(err.Error(), "--pk-font-mono") {
				t.Fatalf("unsupported stack = %#v, %v", got, err)
			}
			if theme != before {
				t.Fatal("refusal mutated source typography")
			}
		})
	}
}

func TestFontFamilyWireMeaningIsExplicit(t *testing.T) {
	t.Parallel()
	theme := design.Light()
	theme.Typography.Body = `"serif", serif`
	families, err := theme.FontFamilies()
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(families[1])
	if err != nil || string(got) != `{"name":"--pk-font-body","families":[{"name":"serif"},{"name":"serif","generic":true}]}` {
		t.Fatalf("ordered family wire contract = %s, %v", got, err)
	}
}

func TestTypedFontFamiliesValidateWithoutReinterpretingLiteralNames(t *testing.T) {
	t.Parallel()
	for _, family := range []design.FontFamily{
		{Name: "Example, Sans"}, {Name: "serif"}, {Name: "  Spaced  Name  "},
		{Name: `Name\With"Punctuation`}, {Name: "ui-serİf"}, {Name: "serif", Generic: true},
	} {
		before := family
		if err := family.Validate(); err != nil || family != before {
			t.Fatalf("valid literal or canonical generic refused or changed: %#v, %v", family, err)
		}
	}
	for _, family := range []design.FontFamily{
		{}, {Name: ""}, {Name: "\xff"}, {Name: "Nul\x00Name"}, {Name: "New\nLine"},
		{Name: "New\rLine"}, {Name: "New\fLine"}, {Name: "Unknown", Generic: true},
		{Name: "SeRiF", Generic: true}, {Name: "ui-serİf", Generic: true}, {Name: "generic(kai)", Generic: true},
	} {
		before := family
		if err := family.Validate(); err == nil || family != before {
			t.Fatalf("invalid typed family accepted or changed: %#v, %v", family, err)
		}
	}
	for _, theme := range design.Default().Both() {
		families, err := theme.FontFamilies()
		if err != nil {
			t.Fatal(err)
		}
		for _, token := range families {
			for _, family := range token.Families {
				if err := family.Validate(); err != nil {
					t.Fatalf("owner projected an invalid typed family: %s: %v", token.Name, err)
				}
			}
		}
	}
}

func TestTypedFontFamilyTokenRequiresIdentityAndCompleteOrderedValues(t *testing.T) {
	t.Parallel()
	valid := design.FontFamilyToken{Name: "--pk-font-body", Families: []design.FontFamily{{Name: "serif"}, {Name: "serif", Generic: true}, {Name: "serif"}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("ordered repeated names are valid and must not be deduplicated: %v", err)
	}
	for _, token := range []design.FontFamilyToken{
		{Name: "--pk-font-body"}, {Name: "font.body", Families: valid.Families},
		{Name: "--pk-font-body", Families: []design.FontFamily{{Name: "Valid"}, {Name: "future", Generic: true}}},
	} {
		before, _ := json.Marshal(token)
		if err := token.Validate(); err == nil {
			t.Fatalf("invalid family token admitted: %#v", token)
		}
		after, _ := json.Marshal(token)
		if string(before) != string(after) {
			t.Fatal("typed token validation mutated input")
		}
	}
}
