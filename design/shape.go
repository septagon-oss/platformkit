package design

import "cmp"

// Shape owns semantic corner values. These are trusted Go-authored CSS lengths,
// not browser input or replacements for ui/style's general radius scale.
// Give both modes a value when a brand shares its shape across light and dark.
type Shape struct {
	ButtonRadius string
	CardRadius   string
	ModalRadius  string
}

func (s Shape) tokens() []Token {
	return []Token{
		{Name: "--pk-radius-button", Type: "dimension", Value: cmp.Or(s.ButtonRadius, "0.375rem")},
		{Name: "--pk-radius-card", Type: "dimension", Value: cmp.Or(s.CardRadius, "0.5rem")},
		{Name: "--pk-radius-modal", Type: "dimension", Value: cmp.Or(s.ModalRadius, "1rem")},
	}
}
