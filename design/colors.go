package design

import (
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ColorValue retains authored meaning: exactly one literal, reference or mix.
// Literals admit hex RGB/RGBA (short or long) and transparent, not arbitrary
// CSS expressions. References name CSS custom properties, never equal values.
// This is a source-owned colour operation, not a general CSS evaluator.
type ColorValue struct {
	Literal   string    `json:"literal,omitempty"`
	Reference string    `json:"reference,omitempty"`
	Mix       *ColorMix `json:"mix,omitempty"`
}

// ColorMix is the existing two-input, premultiplied-alpha sRGB operation.
// FirstPercent is finite and in [0,100]; the second weight is its complement.
// Other colour spaces, missing channels and independently weighted inputs are
// outside this contract. Operand order and references remain authored values.
type ColorMix struct {
	First        ColorValue `json:"first"`
	FirstPercent float64    `json:"firstPercent"`
	Second       ColorValue `json:"second"`
}

// ColorToken is a named source colour declaration. A list is an output of its
// existing owner, not an independent registry or theme configuration layer.
type ColorToken struct {
	Name  string     `json:"name"`
	Value ColorValue `json:"value"`
}

// SRGBA contains non-premultiplied sRGB red, green, blue and alpha in [0,1].
// Channels are not rounded to bytes. An entirely transparent mix is zero.
type SRGBA [4]float64

var colorName = regexp.MustCompile(`^--[a-zA-Z0-9_-]+$`)

// CSS validates and emits the source expression without resolving references.
// It preserves supported literal spelling and rejects ambiguous values and
// cyclic mix pointers. Reference existence/type is checked by ResolveColors.
// Names use the ASCII custom-property subset emitted by PlatformKit.
func (v ColorValue) CSS() (string, error) {
	return v.css(make(map[*ColorMix]bool))
}

func (v ColorValue) css(active map[*ColorMix]bool) (string, error) {
	forms := 0
	for _, present := range []bool{v.Literal != "", v.Reference != "", v.Mix != nil} {
		if present {
			forms++
		}
	}
	if forms != 1 {
		return "", fmt.Errorf("colour requires exactly one literal, reference or mix")
	}
	if v.Literal != "" {
		if _, err := parseColor(v.Literal); err != nil {
			return "", err
		}
		return v.Literal, nil
	}
	if v.Reference != "" {
		if !colorName.MatchString(v.Reference) {
			return "", fmt.Errorf("unsupported colour reference %q", v.Reference)
		}
		return "var(" + v.Reference + ")", nil
	}
	m := v.Mix
	if active[m] || math.IsNaN(m.FirstPercent) || m.FirstPercent < 0 || m.FirstPercent > 100 {
		return "", fmt.Errorf("cyclic mix or invalid first percentage")
	}
	active[m] = true
	defer delete(active, m)
	a, err := m.First.css(active)
	if err != nil {
		return "", err
	}
	b, err := m.Second.css(active)
	if err != nil {
		return "", err
	}
	return "color-mix(in srgb, " + a + " " + strconv.FormatFloat(m.FirstPercent, 'f', -1, 64) + "%, " + b + ")", nil
}

// ResolveColors composes selected theme tokens and derived declarations into
// fresh resolved values, without changing either input. The entire selection
// must be dependency-closed. Missing/wrong-type references, duplicate names,
// cycles and unsupported literals fail with no partial output, even when a
// missing mix operand has zero weight. Non-colour base tokens are not output.
// This preflight accepts trusted Go values, not untrusted JSON or CSS input;
// it establishes no native-provider support, layout or write authority.
func ResolveColors(base []Token, derived []ColorToken) (map[string]SRGBA, error) {
	values := make(map[string]ColorValue)
	kinds := make(map[string]string)
	var names []string
	for _, token := range base {
		if !colorName.MatchString(token.Name) || kinds[token.Name] != "" || token.Type == "" {
			return nil, fmt.Errorf("invalid or duplicate base token %q", token.Name)
		}
		kinds[token.Name] = token.Type
		if token.Type == "color" {
			values[token.Name] = ColorValue{Literal: token.Value}
			names = append(names, token.Name)
		}
	}
	for _, token := range derived {
		if !colorName.MatchString(token.Name) || kinds[token.Name] != "" {
			return nil, fmt.Errorf("invalid or duplicate colour %q", token.Name)
		}
		kinds[token.Name] = "color"
		values[token.Name] = token.Value
		names = append(names, token.Name)
	}
	for _, name := range names {
		if _, err := values[name].CSS(); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	resolved := make(map[string]SRGBA, len(values))
	active := make(map[string]bool)
	var resolve func(string) (SRGBA, error)
	var evaluate func(ColorValue) (SRGBA, error)
	resolve = func(name string) (SRGBA, error) {
		if value, ok := resolved[name]; ok {
			return value, nil
		}
		if kinds[name] != "color" || active[name] {
			return SRGBA{}, fmt.Errorf("missing, non-colour or cyclic reference %q", name)
		}
		active[name] = true
		value, err := evaluate(values[name])
		delete(active, name)
		if err != nil {
			return SRGBA{}, fmt.Errorf("%s: %w", name, err)
		}
		resolved[name] = value
		return value, nil
	}
	evaluate = func(v ColorValue) (SRGBA, error) {
		if v.Literal != "" {
			return parseColor(v.Literal)
		}
		if v.Reference != "" {
			return resolve(v.Reference)
		}
		a, err := evaluate(v.Mix.First)
		if err != nil {
			return SRGBA{}, err
		}
		b, err := evaluate(v.Mix.Second)
		if err != nil {
			return SRGBA{}, err
		}
		weight := v.Mix.FirstPercent / 100
		alphaA, alphaB := a[3]*weight, b[3]*(1-weight)
		out := SRGBA{0, 0, 0, alphaA + alphaB}
		if out[3] != 0 {
			for i := range 3 {
				out[i] = (a[i]*alphaA + b[i]*alphaB) / out[3]
			}
		}
		return out, nil
	}
	for _, name := range names {
		if _, err := resolve(name); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func parseColor(value string) (SRGBA, error) {
	if value == "transparent" {
		return SRGBA{}, nil
	}
	digits, ok := strings.CutPrefix(value, "#")
	if !ok || (len(digits) != 3 && len(digits) != 4 && len(digits) != 6 && len(digits) != 8) {
		return SRGBA{}, fmt.Errorf("unsupported colour literal %q", value)
	}
	if len(digits) < 5 {
		var expanded strings.Builder
		for i := range len(digits) {
			expanded.WriteByte(digits[i])
			expanded.WriteByte(digits[i])
		}
		digits = expanded.String()
	}
	channels, err := hex.DecodeString(digits)
	if err != nil {
		return SRGBA{}, fmt.Errorf("unsupported colour literal %q: %w", value, err)
	}
	out := SRGBA{0, 0, 0, 1}
	for i, channel := range channels {
		out[i] = float64(channel) / 255
	}
	return out, nil
}
