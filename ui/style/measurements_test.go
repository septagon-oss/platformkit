package style_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/style"
)

func TestMeasurementsProjectExistingScalesAndRemainDetached(t *testing.T) {
	got, err := style.Measurements()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, m := range got {
		key := m.Scale + "/" + m.Key
		if seen[key] || m.Validate() != nil {
			t.Fatalf("ambiguous or invalid measurement: %+v", m)
		}
		seen[key] = true
		counts[m.Scale]++
		var class, property string
		switch m.Scale {
		case "spacing":
			class, property = "gap-"+m.Key, "gap"
		case "font-size":
			class, property = "text-"+m.Key, "font-size"
		case "line-height":
			class, property = "text-"+m.Key, "line-height"
		case "font-weight":
			class, property = "font-"+m.Key, "font-weight"
		}
		sheet, err := style.Rules(class)
		if err != nil || !strings.Contains(sheet.CSS(), property+": "+m.Value.String()+m.Unit+";") {
			t.Fatalf("measurement is not the owning emitted value: %+v, %v", m, err)
		}
	}
	want := map[string]int{"spacing": len(style.AllSpacings()) - 2, "font-size": 13, "line-height": 13, "font-weight": 9}
	if !reflect.DeepEqual(counts, want) || seen["spacing/auto"] || seen["spacing/full"] {
		t.Fatalf("incomplete scale or keyword reported as a number: %v", counts)
	}
	again, err := style.Measurements()
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal("source measure order/values are not deterministic")
	}
	got[0].Value = "12345"
	after, err := style.Measurements()
	if err != nil || !reflect.DeepEqual(again, after) {
		t.Fatal("consumer mutation changed the source scale")
	}
}

func TestMeasurementsKeepDimensionsDistinctFromMultipliers(t *testing.T) {
	measures, err := style.Measurements()
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]style.Measurement{}
	for _, m := range measures {
		byKey[m.Scale+"/"+m.Key] = m
	}
	for key, want := range map[string][2]string{
		"spacing/px": {"1", "px"}, "spacing/0": {"0", "px"}, "spacing/0.5": {"0.125", "rem"},
		"spacing/4": {"1", "rem"}, "font-size/xs": {"0.75", "rem"},
		"line-height/xs": {"1", "rem"}, "line-height/5xl": {"1", ""}, "font-weight/semibold": {"600", ""},
	} {
		m := byKey[key]
		if [2]string{m.Value.String(), m.Unit} != want {
			t.Fatalf("%s: got %+v, want %v", key, m, want)
		}
	}
	encoded, err := json.Marshal(byKey["line-height/5xl"])
	if err != nil || string(encoded) != `{"scale":"line-height","key":"5xl","value":1,"unit":""}` {
		t.Fatalf("unitless wire contract: %s, %v", encoded, err)
	}
	for _, m := range []style.Measurement{
		{Scale: "spacing", Key: "4", Value: "1.25", Unit: "rem"},
		{Scale: "font-weight", Key: "semibold", Value: "650", Unit: ""},
	} {
		if err := m.Validate(); err != nil {
			t.Fatalf("valid shape must not be confused with equality to one palette: %v", err)
		}
	}
}

func TestMeasurementsRefuseUnknownOrInvalidMeaning(t *testing.T) {
	base := style.Measurement{Scale: "spacing", Key: "4", Value: "1", Unit: "rem"}
	for name, mutate := range map[string]func(*style.Measurement){
		"unknown scale":    func(m *style.Measurement) { m.Scale = "future" },
		"unknown key":      func(m *style.Measurement) { m.Key = "future" },
		"keyword":          func(m *style.Measurement) { m.Key = "auto" },
		"missing unit":     func(m *style.Measurement) { m.Unit = "" },
		"unknown unit":     func(m *style.Measurement) { m.Unit = "em" },
		"empty value":      func(m *style.Measurement) { m.Value = "" },
		"non-number":       func(m *style.Measurement) { m.Value = "null" },
		"non-JSON number":  func(m *style.Measurement) { m.Value = "01" },
		"infinite":         func(m *style.Measurement) { m.Value = "1e999" },
		"negative":         func(m *style.Measurement) { m.Value = "-1" },
		"dimension weight": func(m *style.Measurement) { m.Scale, m.Key = "font-weight", "semibold" },
		"weight range":     func(m *style.Measurement) { m.Scale, m.Key, m.Unit, m.Value = "font-weight", "semibold", "", "1001" },
	} {
		t.Run(name, func(t *testing.T) {
			m := base
			mutate(&m)
			before := m
			if m.Validate() == nil || m != before {
				t.Fatalf("invalid measurement accepted or mutated: %+v", m)
			}
		})
	}
}
