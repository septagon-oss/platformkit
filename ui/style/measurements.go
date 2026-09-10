package style

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// Measurement projects a named value from the existing numeric style scales.
// Scale and Key identify it independently of order. Unit is px/rem for a length
// or explicitly empty for a number (weight or line-height multiplier). Decimal
// source encoding is retained; this is not a pixel measurement or a font asset.
type Measurement struct {
	Scale string      `json:"scale"`
	Key   string      `json:"key"`
	Value json.Number `json:"value"`
	Unit  string      `json:"unit"`
}

// Validate checks the source-measurements.v1 domain, not equality to the default
// values, rendered equivalence or provenance. A valid measurement grants no
// authority to change the stylesheet or introduce a new scale/key.
func (m Measurement) Validate() error {
	n, err := m.Value.Float64()
	valid := err == nil && json.Valid([]byte(m.Value)) && !math.IsInf(n, 0) && n >= 0
	length := m.Unit == "px" || m.Unit == "rem"
	switch m.Scale {
	case "spacing":
		valid = valid && length && slices.Contains(AllSpacings(), Spacing(m.Key)) && m.Key != "auto" && m.Key != "full"
	case "font-size":
		valid = valid && length && slices.Contains(AllFontSizes(), FontSize(m.Key))
	case "line-height":
		valid = valid && (length || m.Unit == "") && slices.Contains(AllFontSizes(), FontSize(m.Key))
	case "font-weight":
		valid = valid && m.Unit == "" && n >= 1 && n <= 1000 && slices.Contains(AllFontWeights(), FontWeight(m.Key))
	default:
		valid = false
	}
	if !valid {
		return fmt.Errorf("style: invalid measurement %q/%q", m.Scale, m.Key)
	}
	return nil
}

// Measurements projects the same functions/tables used by CSS emission. It
// reads no HTML/CSS input, creates no registry and changes no caller values.
// Spacing keywords auto/full are not numbers and are deliberately excluded.
func Measurements() ([]Measurement, error) {
	var out []Measurement
	add := func(scale, key, literal string) error {
		m := Measurement{Scale: scale, Key: key, Value: json.Number(literal)}
		for _, unit := range []string{"px", "rem"} {
			if value, ok := strings.CutSuffix(literal, unit); ok {
				m.Value, m.Unit = json.Number(value), unit
				break
			}
		}
		if err := m.Validate(); err != nil {
			return err
		}
		out = append(out, m)
		return nil
	}
	for _, step := range AllSpacings() {
		if step == SAuto || step == SFull {
			continue
		}
		value, ok := spacingCSS(string(step))
		if !ok {
			return nil, fmt.Errorf("style: spacing %q has no source value", step)
		}
		if err := add("spacing", string(step), value); err != nil {
			return nil, err
		}
	}
	for _, step := range AllFontSizes() {
		values := fontSizes[string(step)]
		for i, scale := range []string{"font-size", "line-height"} {
			if err := add(scale, string(step), values[i]); err != nil {
				return nil, err
			}
		}
	}
	for _, step := range AllFontWeights() {
		if err := add("font-weight", string(step), fontWeights[string(step)]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
