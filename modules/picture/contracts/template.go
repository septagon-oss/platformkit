// Package contracts owns inert picture declarations and the pure rendering decision.
package contracts

import (
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
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

// Compile validates bounded UTF-8 XML against the closed SVG grammar. A refusal
// returns no usable declaration and names the offending construct, never the source.
func Compile(name, source string) (Template, error) {
	fail := func(c, r string) (Template, error) { return Template{}, refusal(name, c, r) }
	if name == "" || len(name) > 80 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return fail("declaration label", "use 1–80 UTF-8 bytes without controls")
	}
	if len(source) > 262144 {
		return fail("XML document", "source exceeds 262144 bytes")
	}
	if !utf8.ValidString(source) || strings.HasPrefix(source, "\ufeff") || strings.ContainsFunc(source, forbiddenXML) {
		return fail("XML document", "XML 1.0 UTF-8 without a BOM is required")
	}
	d := xml.NewDecoder(strings.NewReader(source))
	decl := &declaration{digest: sha256.Sum256([]byte(source))}
	var stack []*element
	parts := map[string]map[string]bool{}
	count, numbers, optionDepth := 0, 0, 0
	seenToken, xmlDecl := false, false
	for {
		token, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			if strings.Contains(err.Error(), "unsupported version") {
				return fail("XML processing instruction", "only XML declaration version 1.0 is allowed")
			}
			return fail("XML document", "malformed XML or unknown entity")
		}
		switch tok := token.(type) {
		case xml.StartElement:
			count++
			if count > 2048 || len(stack) >= 32 {
				return fail(tok.Name.Local, "maximum 2048 elements and depth 32")
			}
			if tok.Name.Space != "" {
				return fail(tok.Name.Local, "namespace prefixes are forbidden")
			}
			e := &element{name: tok.Name.Local, attrs: map[string]string{}}
			if len(tok.Attr) > 20 {
				return fail(e.name, "maximum 20 attributes")
			}
			for _, a := range tok.Attr {
				if a.Name.Space != "" {
					return fail(e.name+" "+a.Name.Space+":"+a.Name.Local, "namespace prefixes are forbidden")
				}
				if _, ok := e.attrs[a.Name.Local]; ok {
					return fail(e.name+" "+a.Name.Local, "duplicate attribute")
				}
				if len(a.Value) > 65536 {
					return fail(e.name+" "+a.Name.Local, "attribute exceeds 65536 bytes")
				}
				e.attrs[a.Name.Local] = a.Value
			}
			if len(stack) == 0 {
				if decl.root != nil || e.name != "svg" {
					return fail(e.name, "exactly one svg root is required")
				}
				if e.attrs["xmlns"] != svgNamespace {
					return fail(e.name, "required SVG namespace")
				}
				decl.root = e
			} else {
				if _, ok := e.attrs["xmlns"]; ok {
					return fail(e.name, "namespace declarations are root-only")
				}
				stack[len(stack)-1].children = append(stack[len(stack)-1].children, e)
			}
			if err := validateElement(e, len(stack) == 0, &numbers); err != nil {
				return fail(e.name, err.Error())
			}
			part, opt := e.attrs["data-part"], e.attrs["data-option"]
			_, hasPart := e.attrs["data-part"]
			_, hasOpt := e.attrs["data-option"]
			if hasPart != hasOpt {
				return fail(e.name, "data-part and data-option must appear together")
			}
			if hasPart {
				if !validName(part) || !validName(opt) {
					return fail(e.name, "invalid data-part or data-option name")
				}
				if optionDepth != 0 {
					return fail(e.name, "nested option-bearing elements are forbidden")
				}
				optionDepth = len(stack) + 1
				if parts[part] == nil {
					parts[part] = map[string]bool{}
				}
				parts[part][opt] = true
				if len(parts) > 32 || len(parts[part]) > 64 {
					return fail(e.name, "maximum 32 parts and 64 options per part")
				}
			}
			if slot, ok := e.attrs["data-slot"]; ok {
				slot = strings.TrimPrefix(slot, "stroke:")
				if !validName(slot) {
					return fail(e.name, "invalid data-slot name")
				}
				if !slices.Contains(decl.slots, slot) {
					decl.slots = append(decl.slots, slot)
				}
				if len(decl.slots) > 16 {
					return fail(e.name, "maximum 16 slots")
				}
			}
			stack = append(stack, e)
		case xml.EndElement:
			if len(stack) == 0 || tok.Name.Space != "" || stack[len(stack)-1].name != tok.Name.Local {
				return fail("XML document", "mismatched closing element")
			}
			if optionDepth == len(stack) {
				optionDepth = 0
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if strings.Trim(string(tok), " \t\r\n") != "" {
				c := "XML document"
				if len(stack) > 0 {
					c = stack[len(stack)-1].name
				}
				return fail(c, "character data is forbidden")
			}
		case xml.Comment:
		case xml.Directive:
			return fail("XML document", "directives and DOCTYPE are forbidden")
		case xml.ProcInst:
			if seenToken || xmlDecl || tok.Target != "xml" || !xmlDeclaration.Match(tok.Inst) {
				return fail("XML processing instruction", "only an initial XML 1.0 UTF-8 declaration is allowed")
			}
			xmlDecl = true
		default:
			return fail("XML document", "unsupported XML construct")
		}
		seenToken = true
	}
	if decl.root == nil || len(stack) != 0 {
		return fail("XML document", "one complete svg root is required")
	}
	for _, p := range slices.Sorted(maps.Keys(parts)) {
		decl.parts = append(decl.parts, Part{Name: p, Options: slices.Sorted(maps.Keys(parts[p]))})
	}
	decl.box, _ = numericList(decl.root.attrs["viewBox"], nil)
	return Template{data: decl}, nil
}
