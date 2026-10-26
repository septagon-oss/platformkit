// Package picture validates and renders bounded inert SVG values without I/O.
package contracts

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const svgNamespace = "http://www.w3.org/2000/svg"

// Template is an immutable validated declaration. Its zero value is unusable.
type Template struct{ data *declaration }
type declaration struct {
	digest [32]byte
	root   *element
	parts  []Part
	slots  []string
	box    []float64
}
type element struct {
	name     string
	attrs    map[string]string
	children []*element
	text     string
}

// Part names a selection and its byte-sorted allowed options.
type Part struct {
	Name    string
	Options []string
}

// Templates is the application's finite declaration set, limited to 64 distinct digests.
type Templates []Template

// Digest identifies the exact submitted bytes, independently of the diagnostic label.
func (t Template) Digest() [32]byte {
	if t.data == nil {
		return [32]byte{}
	}
	return t.data.digest
}

// Parts returns detached, byte-sorted selections and options.
func (t Template) Parts() []Part {
	if t.data == nil {
		return nil
	}
	out := slices.Clone(t.data.parts)
	for i := range out {
		out[i].Options = slices.Clone(out[i].Options)
	}
	return out
}

// Slots returns a detached vocabulary in document preorder.
func (t Template) Slots() []string {
	if t.data == nil {
		return nil
	}
	return slices.Clone(t.data.slots)
}

func refusal(label, construct, reason string) error {
	clean := func(s string, limit int) string {
		s = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' '
			}
			return r
		}, s)
		if len(s) > limit {
			s = s[:limit]
			for !utf8.ValidString(s) {
				s = s[:len(s)-1]
			}
			s += "…"
		}
		return s
	}
	message := fmt.Sprintf("picture %q: %s: %s", clean(label, 32), clean(construct, 60), clean(reason, 70))
	return fmt.Errorf("%s.", clean(message, 252))
}

func forbiddenXML(r rune) bool {
	return (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0xfffe || r == 0xffff
}

// Compile validates bounded UTF-8 XML against the composed-template profile.
// Refusals return no usable declaration and never echo the source.
func Compile(name, source string) (Template, error) {
	decl, err := compile(name, source, false)
	if err != nil {
		return Template{}, err
	}
	return Template{data: decl}, nil
}
