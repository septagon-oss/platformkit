package design

import (
	"cmp"
	"fmt"
	"regexp"
)

// Shape owns semantic corner values. These are trusted Go-authored CSS lengths,
// not browser input or replacements for ui/style's general radius scale.
// Give both modes a value when a brand shares its shape across light and dark.
//
// The tags are the spelling a person writes: a client's design.yaml says
// card-radius, not cardradius, and the loader decodes with KnownFields, so an
// untagged field here would refuse the readable form and accept the unreadable
// one.
type Shape struct {
	ButtonRadius string `yaml:"button-radius,omitempty"`
	CardRadius   string `yaml:"card-radius,omitempty"`
	ModalRadius  string `yaml:"modal-radius,omitempty"`
}

func (s Shape) tokens() []Token {
	out := make([]Token, 0, len(shapeFields))
	for _, field := range shapeFields {
		out = append(out, Token{Name: field.token, Type: "dimension", Value: cmp.Or(field.value(s), field.fallback)})
	}
	return out
}

// radiusLength is the whole of the length grammar a shape may carry: an unsigned
// decimal and one of the two units that mean the same thing at every viewport.
// A percentage radius means a different shape in a different container, and the
// DTCG document a client ships carries an absolute dimension; calc() would mean
// this package parses CSS. No exponent: 1e3rem is a length nobody measured.
var radiusLength = regexp.MustCompile(`^([0-9]{1,4}(?:\.[0-9]{1,3})?)(px|rem)$`)

// ParseRadius splits one shape length into its number and unit, in the units a
// DTCG dimension carries. It is exported for ui/export, which projects the three
// radii into the document mobile reads and must not keep a second grammar for
// what a radius is.
func ParseRadius(value string) (number, unit string, err error) {
	match := radiusLength.FindStringSubmatch(value)
	if match == nil {
		return "", "", fmt.Errorf("radius %q is not one: a radius is a length in px or rem", value)
	}
	return match[1], match[2], nil
}

// shapeFields is the shape vocabulary in one list: the token each radius is
// exported as, the name a person writes and a refusal repeats, how to read the
// field, and what the theme ships when the client says nothing. Tokens projects
// it, Validate reads it and ui/export checks the identities it takes against it,
// so a radius nothing reads cannot be added and one a client may name cannot be
// hidden — the same discipline colorFields is for colour.
var shapeFields = []struct {
	token    string
	field    string
	value    func(Shape) string
	fallback string
}{
	{"--pk-radius-button", "button-radius", func(s Shape) string { return s.ButtonRadius }, "0.375rem"},
	{"--pk-radius-card", "card-radius", func(s Shape) string { return s.CardRadius }, "0.5rem"},
	{"--pk-radius-modal", "modal-radius", func(s Shape) string { return s.ModalRadius }, "1rem"},
}

// RadiusTokenNames lists the three radius tokens a theme projects, in the order
// Tokens emits them. The projection that ships them to a design tool checks its
// selection against this list rather than carrying a second copy of the list.
func RadiusTokenNames() []string {
	out := make([]string, 0, len(shapeFields))
	for _, field := range shapeFields {
		out = append(out, field.token)
	}
	return out
}

// Validate refuses a radius that is not a length in px or rem. An unset radius
// is not a refusal: the token it would have set keeps the kernel's own value.
func (s Shape) Validate() error {
	for _, field := range shapeFields {
		if value := field.value(s); value != "" {
			if _, _, err := ParseRadius(value); err != nil {
				return fmt.Errorf("shape: %s: %w", field.field, err)
			}
		}
	}
	return nil
}
