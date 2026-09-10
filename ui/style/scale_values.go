package style

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Scalar is a decimal number with an explicit unit, empty for a multiplier.
// Contextual units retain their meaning; this value does not resolve pixels.
type Scalar struct {
	Value json.Number `json:"value"`
	Unit  string      `json:"unit"`
}

func (s Scalar) number(units ...string) (float64, bool) {
	n, err := s.Value.Float64()
	return n, err == nil && json.Valid([]byte(s.Value)) && !math.IsInf(n, 0) && slices.Contains(units, s.Unit)
}

// ScaleValue names exactly one number or keyword from an existing style scale.
// It extends the source projection, not the source-measurements.v1 wire domain.
// Shape validation is not authority to configure a scale or mutate source.
type ScaleValue struct {
	Scale   string  `json:"scale"`
	Key     string  `json:"key"`
	Number  *Scalar `json:"number,omitempty"`
	Keyword string  `json:"keyword,omitempty"`
}

// Validate admits the owned scale keys and supported source units. It does not
// require equality to current defaults or claim that a contextual unit has the
// same value across fonts, root sizes, containers or viewports.
func (v ScaleValue) Validate() error {
	valid := false
	if v.Number == nil {
		valid = (v.Scale == "spacing" && v.Key == "auto" && v.Keyword == "auto") ||
			(v.Scale == "max-width" && v.Key == "none" && v.Keyword == "none")
	} else if v.Keyword == "" {
		n, length := v.Number.number("px", "rem", "em", "ch", "vw")
		nonnegative := n >= 0 && !negativeNumber(v.Number.Value)
		switch v.Scale {
		case "tracking":
			valid = length && trackings[v.Key] != ""
		case "leading":
			_, unitless := v.Number.number("")
			valid = unitless && nonnegative && leadings[v.Key] != ""
		case "radius":
			valid = length && nonnegative && slices.Contains(AllRadii(), Radius(v.Key))
		case "max-width":
			_, dimension := v.Number.number("px", "rem", "em", "ch", "vw", "%")
			valid = dimension && nonnegative && v.Key != "none" && maxWidths[v.Key] != ""
		case "breakpoint":
			_, dimension := v.Number.number("px", "rem", "em")
			valid = dimension && nonnegative && breakpoints[v.Key] != ""
		case "duration":
			_, time := v.Number.number("ms", "s")
			valid = time && nonnegative && slices.Contains(AllDurations(), Duration(v.Key))
		case "spacing":
			if v.Key == "full" {
				_, percentage := v.Number.number("%")
				valid = percentage && nonnegative
				break
			}
			fallthrough
		default:
			valid = (Measurement{Scale: v.Scale, Key: v.Key, Value: v.Number.Value, Unit: v.Number.Unit}).Validate() == nil
		}
	}
	if !valid {
		return fmt.Errorf("style: invalid scale value %q/%q", v.Scale, v.Key)
	}
	return nil
}

// ScaleValues projects the existing scale owners, including spacing keywords,
// tracking, leading, radii, maximum widths, all supported breakpoints and
// duration steps. Breakpoints remain declared even without a matching viewport.
// Legacy Measurements and snapshot bytes remain unchanged. Shadows and timing
// composites have their own projections; no rendered CSS is reverse-engineered.
func ScaleValues() ([]ScaleValue, error) {
	measurements, err := Measurements()
	if err != nil {
		return nil, err
	}
	out := make([]ScaleValue, 0, len(measurements))
	for _, m := range measurements {
		out = append(out, ScaleValue{Scale: m.Scale, Key: m.Key, Number: &Scalar{Value: m.Value, Unit: m.Unit}})
	}
	add := func(scale, key, literal string) {
		v := ScaleValue{Scale: scale, Key: key}
		if literal == "auto" || literal == "none" {
			v.Keyword = literal
		} else {
			n := Scalar{Value: json.Number(literal)}
			for _, unit := range []string{"rem", "px", "em", "ch", "vw", "%", "ms", "s"} {
				if value, ok := strings.CutSuffix(literal, unit); ok {
					n.Value, n.Unit = json.Number(value), unit
					break
				}
			}
			v.Number = &n
		}
		out = append(out, v)
	}
	for _, step := range []Spacing{SAuto, SFull} {
		value, _ := spacingCSS(string(step))
		add("spacing", string(step), value)
	}
	for _, table := range []struct {
		scale  string
		values map[string]string
	}{
		{"tracking", trackings}, {"leading", leadings}, {"radius", radii},
		{"max-width", maxWidths}, {"breakpoint", breakpoints},
	} {
		for _, key := range slices.Sorted(maps.Keys(table.values)) {
			name := key
			if name == "" {
				name = "base"
			}
			add(table.scale, name, table.values[key])
		}
	}
	for _, step := range AllDurations() {
		value, _ := durationCSS(string(step))
		add("duration", string(step), value)
	}
	for _, v := range out {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}
	return out, nil
}
