package style

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/design"
)

// ShadowLayer preserves one ordered inset/outset shadow. Length units are
// explicit, including zero. Empty Blur/Spread mean omitted optional lengths;
// spread cannot be supplied without blur. Colour retains non-byte alpha.
type ShadowLayer struct {
	OffsetX Scalar       `json:"offsetX"`
	OffsetY Scalar       `json:"offsetY"`
	Blur    Scalar       `json:"blur,omitzero"`
	Spread  Scalar       `json:"spread,omitzero"`
	Color   design.SRGBA `json:"color"`
	Inset   bool         `json:"inset,omitzero"`
}

// ShadowValue names an existing shadow step and preserves its paint-order list.
type ShadowValue struct {
	Key    string        `json:"key"`
	Layers []ShadowLayer `json:"layers"`
}

// Validate checks the source shape, not equality to current shadow defaults.
func (v ShadowValue) Validate() error {
	valid := slices.Contains(AllShadows(), Shadow(v.Key)) && len(v.Layers) > 0
	for _, layer := range v.Layers {
		for _, length := range []Scalar{layer.OffsetX, layer.OffsetY} {
			_, ok := length.number("px", "rem", "em", "ch", "vw")
			valid = valid && ok
		}
		if layer.Blur != (Scalar{}) {
			n, ok := layer.Blur.number("px", "rem", "em", "ch", "vw")
			valid = valid && ok && n >= 0
		}
		if layer.Spread != (Scalar{}) {
			_, ok := layer.Spread.number("px", "rem", "em", "ch", "vw")
			valid = valid && ok && layer.Blur != (Scalar{})
		}
		valid = valid && layer.Color.Validate() == nil
	}
	if !valid {
		return fmt.Errorf("style: invalid shadow %q", v.Key)
	}
	return nil
}

// ShadowValues projects detached layers from the same table that emits CSS.
// The source's shadow-none is a transparent layer, not an absent declaration.
func ShadowValues() ([]ShadowValue, error) {
	var out []ShadowValue
	for _, key := range AllShadows() {
		lookup := string(key)
		if key == ShadowBase {
			lookup = ""
		}
		v := ShadowValue{Key: string(key), Layers: slices.Clone(shadows[lookup])}
		if err := v.Validate(); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func shadowLayer(x, y, blur, spread int, alpha float64, inset bool) ShadowLayer {
	length := func(value int) Scalar { return Scalar{Value: json.Number(strconv.Itoa(value)), Unit: "px"} }
	return ShadowLayer{OffsetX: length(x), OffsetY: length(y), Blur: length(blur), Spread: length(spread), Color: design.SRGBA{0, 0, 0, alpha}, Inset: inset}
}

func (l ShadowLayer) css() string {
	var parts []string
	if l.Inset {
		parts = append(parts, "inset")
	}
	for _, length := range []Scalar{l.OffsetX, l.OffsetY, l.Blur, l.Spread} {
		if length == (Scalar{}) {
			continue
		}
		value := length.Value.String()
		if value != "0" {
			value += length.Unit
		}
		parts = append(parts, value)
	}
	color := "#0000"
	if l.Color != (design.SRGBA{}) {
		color = "rgb(" + decimal(l.Color[0]*255) + " " + decimal(l.Color[1]*255) + " " + decimal(l.Color[2]*255) + " / " + decimal(l.Color[3]) + ")"
	}
	return strings.Join(append(parts, color), " ")
}

func decimal(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
