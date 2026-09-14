package style_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestScaleValuesRetainUnitsKeywordsAndLegacyMeasurements(t *testing.T) {
	values, err := style.ScaleValues()
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]style.ScaleValue)
	for _, value := range values {
		key := value.Scale + "/" + value.Key
		if _, duplicate := byKey[key]; duplicate || value.Validate() != nil {
			t.Fatalf("duplicate or invalid value: %+v", value)
		}
		byKey[key] = value
	}
	for key, want := range map[string][2]string{
		"spacing/0.5": {"0.125", "rem"}, "spacing/full": {"100", "%"},
		"tracking/tighter": {"-0.05", "em"}, "leading/normal": {"1.5", ""},
		"radius/base": {"0.25", "rem"}, "radius/full": {"9999", "px"},
		"max-width/prose": {"65", "ch"}, "max-width/screen": {"100", "vw"},
		"max-width/full": {"100", "%"}, "breakpoint/md": {"768", "px"},
		"duration/150": {"150", "ms"}, "line-height/xs": {"1", "rem"},
		"line-height/5xl": {"1", ""},
	} {
		v := byKey[key]
		if v.Number == nil || [2]string{v.Number.Value.String(), v.Number.Unit} != want || v.Keyword != "" {
			t.Errorf("%s: got %+v, want number/unit %v", key, v, want)
		}
	}
	for key, want := range map[string]string{"spacing/auto": "auto", "max-width/none": "none"} {
		if v := byKey[key]; v.Keyword != want || v.Number != nil {
			t.Errorf("%s lost keyword meaning: %+v", key, v)
		}
	}
	legacy, err := style.Measurements()
	if err != nil || len(values) != len(legacy)+51 {
		t.Fatalf("incomplete owned scales: %d, legacy %d, %v", len(values), len(legacy), err)
	}
	for _, m := range legacy {
		value := byKey[m.Scale+"/"+m.Key]
		if value.Number == nil || *value.Number != (style.Scalar{Value: m.Value, Unit: m.Unit}) {
			t.Fatalf("legacy value changed: %+v", m)
		}
	}
	before, _ := json.Marshal(values)
	values[0].Number.Value = "9999"
	again, err := style.ScaleValues()
	after, _ := json.Marshal(again)
	if err != nil || string(before) != string(after) {
		t.Fatal("scale projection is nondeterministic or shares caller-mutable numbers")
	}
}

func TestScaleValuesAgreeWithOwningCSSAndInactiveBreakpoints(t *testing.T) {
	values, err := style.ScaleValues()
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		class, property := "", ""
		switch value.Scale {
		case "spacing":
			class, property = "w-"+value.Key, "width"
		case "font-size":
			class, property = "text-"+value.Key, "font-size"
		case "line-height":
			class, property = "text-"+value.Key, "line-height"
		case "font-weight":
			class, property = "font-"+value.Key, "font-weight"
		case "tracking":
			class, property = "tracking-"+value.Key, "letter-spacing"
		case "leading":
			class, property = "leading-"+value.Key, "line-height"
		case "radius":
			class, property = "rounded-"+value.Key, "border-radius"
			if value.Key == "base" {
				class = "rounded"
			}
		case "max-width":
			class, property = "max-w-"+value.Key, "max-width"
		case "duration":
			class, property = "duration-"+value.Key, "transition-duration"
		case "breakpoint":
			class, property = value.Key+":block", "min-width"
		default:
			t.Fatalf("unverified source scale %q", value.Scale)
		}
		literal := value.Keyword
		if value.Number != nil {
			literal = value.Number.Value.String() + value.Number.Unit
		}
		end := ";"
		if value.Scale == "breakpoint" {
			end = ")"
		}
		sheet, err := style.Rules(class)
		if err != nil || !strings.Contains(sheet.CSS(), property+": "+literal+end) {
			t.Errorf("%s/%s differs from owning %s: %v", value.Scale, value.Key, class, err)
		}
	}
}

func TestNumericBreakpointUsesAValidCSSIdentifier(t *testing.T) {
	sheet, err := style.Rules("2xl:block", "2xl:hover:block")
	if err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{`.\32 xl\:block {`, `.\32 xl\:hover\:block:hover {`} {
		if !strings.Contains(sheet.CSS(), selector) {
			t.Errorf("numeric prefix selector is not escaped as a CSS identifier: missing %s", selector)
		}
	}
}

func TestScaleValuesRefuseMixedOrInvalidKindsWithoutWideningV1(t *testing.T) {
	for _, value := range []style.ScaleValue{
		{Scale: "tracking", Key: "tighter", Number: &style.Scalar{Value: "-0.05", Unit: "em"}},
		{Scale: "radius", Key: "base", Number: &style.Scalar{Value: "0.5", Unit: "rem"}},
		{Scale: "font-weight", Key: "semibold", Number: &style.Scalar{Value: "650"}},
	} {
		if err := value.Validate(); err != nil {
			t.Fatalf("valid source value confused with equality to current defaults: %v", err)
		}
	}
	for _, value := range []style.ScaleValue{
		{Scale: "tracking", Key: "tighter", Number: &style.Scalar{Value: "-0.05", Unit: "%"}},
		{Scale: "leading", Key: "normal", Number: &style.Scalar{Value: "1.5", Unit: "px"}},
		{Scale: "radius", Key: "base", Number: &style.Scalar{Value: "-1", Unit: "px"}},
		{Scale: "radius", Key: "future", Number: &style.Scalar{Value: "1", Unit: "px"}},
		{Scale: "spacing", Key: "auto", Number: &style.Scalar{Value: "1", Unit: "px"}},
		{Scale: "spacing", Key: "full", Number: &style.Scalar{Value: "100", Unit: "px"}},
		{Scale: "spacing", Key: "4", Keyword: "auto"},
		{Scale: "max-width", Key: "none", Number: &style.Scalar{Value: "0", Unit: "px"}},
		{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "-1", Unit: "ms"}},
		{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "null", Unit: "ms"}},
		{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "01", Unit: "ms"}},
		{Scale: "duration", Key: "150", Number: &style.Scalar{Value: "1e999", Unit: "ms"}},
		{Scale: "spacing", Key: "auto", Keyword: "auto", Number: &style.Scalar{Value: "1"}},
		{Scale: "future", Key: "4", Number: &style.Scalar{Value: "1", Unit: "px"}},
		{Scale: "spacing", Key: "4"},
	} {
		before, _ := json.Marshal(value)
		err := value.Validate()
		after, _ := json.Marshal(value)
		if err == nil || !reflect.DeepEqual(before, after) {
			t.Errorf("invalid value accepted or mutated: %+v", value)
		}
	}
	if (style.Measurement{Scale: "tracking", Key: "tighter", Value: "-0.05", Unit: "em"}).Validate() == nil {
		t.Fatal("source-measurements.v1 silently accepted new meaning")
	}
}
