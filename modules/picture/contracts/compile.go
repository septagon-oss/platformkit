package contracts

import (
	"crypto/sha256"
	"encoding/xml"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type constructError struct {
	construct string
	error
}

type compiler struct {
	declaration
	stack                                  []*element
	parts                                  map[string]map[string]bool
	count, numbers, markerDepth, textBytes int
	figure, seenToken, xmlDecl             bool
}

func compile(name, source string, figure bool) (*declaration, error) {
	if err := sourceFault(name, source); err != nil {
		return nil, err
	}
	p := compiler{declaration: declaration{digest: sha256.Sum256([]byte(source))}, parts: map[string]map[string]bool{}, figure: figure}
	d := xml.NewDecoder(strings.NewReader(source))
	for {
		token, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, refusal(name, "XML document", "malformed XML or unknown entity")
		}
		if err := p.token(token); err != nil {
			if problem, ok := errors.AsType[*constructError](err); ok {
				return nil, refusal(name, problem.construct, problem.Error())
			}
			return nil, refusal(name, "XML document", err.Error())
		}
		p.seenToken = true
	}
	if p.root == nil || len(p.stack) != 0 {
		return nil, refusal(name, "XML document", "one complete svg root is required")
	}
	for _, part := range slices.Sorted(maps.Keys(p.parts)) {
		p.partsSorted(part)
	}
	p.box, _ = numericList(p.root.attrs["viewBox"], nil)
	return &p.declaration, nil
}

func sourceFault(name, source string) error {
	if name == "" || len(name) > 80 || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return refusal(name, "declaration label", "use 1–80 UTF-8 bytes without controls")
	}
	if len(source) > 262144 {
		return refusal(name, "XML document", "source exceeds 262144 bytes")
	}
	if !utf8.ValidString(source) || strings.HasPrefix(source, "\ufeff") || strings.ContainsFunc(source, forbiddenXML) {
		return refusal(name, "XML document", "XML 1.0 UTF-8 without a BOM is required")
	}
	if initial, ok := strings.CutPrefix(source, "<?xml"); ok {
		instruction, _, closed := strings.Cut(initial, "?>")
		if !closed || !xmlDeclaration.MatchString(strings.TrimSpace(instruction)) {
			return refusal(name, "XML processing instruction", "only an initial XML 1.0 UTF-8 declaration is allowed")
		}
	}
	return nil
}

func (p *compiler) token(token xml.Token) error {
	switch tok := token.(type) {
	case xml.StartElement:
		return p.start(tok)
	case xml.EndElement:
		if len(p.stack) == 0 || tok.Name.Space != "" || p.stack[len(p.stack)-1].name != tok.Name.Local {
			return errors.New("mismatched closing element")
		}
		if p.markerDepth == len(p.stack) {
			p.markerDepth = 0
		}
		p.stack = p.stack[:len(p.stack)-1]
	case xml.CharData:
		if p.figure && len(p.stack) > 0 && p.stack[len(p.stack)-1].name == "text" {
			e := p.stack[len(p.stack)-1]
			e.text += string(tok)
			p.textBytes += len(tok)
			if len(e.text) > 1024 || p.textBytes > 16384 {
				return errors.New("text exceeds 1024 bytes per element or 16384 total")
			}
		} else if strings.Trim(string(tok), " \t\r\n") != "" {
			return errors.New("character data is forbidden")
		}
	case xml.Comment:
	case xml.Directive:
		return errors.New("directives and DOCTYPE are forbidden")
	case xml.ProcInst:
		if p.seenToken || p.xmlDecl || tok.Target != "xml" || !xmlDeclaration.Match(tok.Inst) {
			return errors.New("XML processing instruction: only an initial XML 1.0 UTF-8 declaration is allowed")
		}
		p.xmlDecl = true
	default:
		return errors.New("unsupported XML construct")
	}
	return nil
}

func (p *compiler) start(tok xml.StartElement) error {
	fail := func(message string) error { return &constructError{tok.Name.Local, errors.New(message)} }
	p.count++
	if p.count > 2048 || len(p.stack) >= 32 {
		return fail("maximum 2048 elements and depth 32")
	}
	if tok.Name.Space != "" {
		return fail("namespace prefixes are forbidden")
	}
	e := &element{name: tok.Name.Local, attrs: map[string]string{}}
	if len(tok.Attr) > 20 {
		return fail("maximum 20 attributes")
	}
	for _, a := range tok.Attr {
		if a.Name.Space != "" {
			return &constructError{e.name + " " + a.Name.Space + ":" + a.Name.Local, errors.New("namespace prefixes are forbidden")}
		}
		if _, ok := e.attrs[a.Name.Local]; ok {
			return fail("duplicate attribute")
		}
		if len(a.Value) > 65536 {
			return fail("attribute exceeds 65536 bytes")
		}
		e.attrs[a.Name.Local] = a.Value
	}
	if len(p.stack) == 0 {
		if p.root != nil || e.name != "svg" {
			return fail("exactly one svg root is required")
		}
		if e.attrs["xmlns"] != svgNamespace {
			return fail("required SVG namespace")
		}
		p.root = e
	} else {
		if _, ok := e.attrs["xmlns"]; ok {
			return fail("namespace declarations are root-only")
		}
		parent := p.stack[len(p.stack)-1]
		if parent.name == "text" {
			return fail("text must have no children")
		}
		parent.children = append(parent.children, e)
	}
	if err := validateElement(e, len(p.stack) == 0, &p.numbers, p.figure); err != nil {
		return &constructError{e.name, err}
	}
	if err := p.markers(e); err != nil {
		return &constructError{e.name, err}
	}
	p.stack = append(p.stack, e)
	return nil
}

func (p *compiler) markers(e *element) error {
	if p.figure {
		if id, ok := e.attrs["data-choice"]; ok {
			if !validName(id) {
				return errors.New("invalid data-choice name")
			}
			return p.markDepth()
		}
		return nil
	}
	part, opt := e.attrs["data-part"], e.attrs["data-option"]
	_, hasPart := e.attrs["data-part"]
	_, hasOpt := e.attrs["data-option"]
	if hasPart != hasOpt {
		return errors.New("data-part and data-option must appear together")
	}
	if hasPart {
		if !validName(part) || !validName(opt) {
			return errors.New("invalid data-part or data-option name")
		}
		if err := p.markDepth(); err != nil {
			return err
		}
		if p.parts[part] == nil {
			p.parts[part] = map[string]bool{}
		}
		p.parts[part][opt] = true
		if len(p.parts) > 32 || len(p.parts[part]) > 64 {
			return errors.New("maximum 32 parts and 64 options per part")
		}
	}
	if slot, ok := e.attrs["data-slot"]; ok {
		slot = strings.TrimPrefix(slot, "stroke:")
		if !validName(slot) {
			return errors.New("invalid data-slot name")
		}
		if !slices.Contains(p.slots, slot) {
			p.slots = append(p.slots, slot)
		}
		if len(p.slots) > 16 {
			return errors.New("maximum 16 slots")
		}
	}
	return nil
}

func (p *compiler) markDepth() error {
	if p.markerDepth != 0 {
		return errors.New("nested option-bearing elements are forbidden")
	}
	p.markerDepth = len(p.stack) + 1
	return nil
}

func (p *compiler) partsSorted(part string) {
	p.declaration.parts = append(p.declaration.parts, Part{Name: part, Options: slices.Sorted(maps.Keys(p.parts[part]))})
}
