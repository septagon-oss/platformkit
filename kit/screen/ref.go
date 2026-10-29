// Package screen describes stable views and controls independently of a renderer.
// References carry no record, tenant, translated text or DOM identity.
package screen

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ViewRef names a generic or custom view. A conversion does not validate it.
type ViewRef string

// AnchorRef names one control in one view. Repeated rows need an instance
// binding and cannot be named by this contract.
type AnchorRef string

// Interaction describes a control's supported interaction, not a domain event.
type Interaction string

const (
	Activated Interaction = "activated"
	Committed Interaction = "committed"
	Submitted Interaction = "submitted"
)

func safe(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-'
}

// Segment encodes a nonempty UTF-8 name without normalizing its spelling.
// Control characters are refused; every unsafe byte uses uppercase percent hex.
func Segment(name string) (string, error) {
	if name == "" || !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return "", fmt.Errorf("screen: invalid name %q", name)
	}
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(name) {
		c := name[i]
		if safe(c) {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String(), nil
}

func decode(segment string) (string, error) {
	name, err := url.PathUnescape(segment)
	if err != nil {
		return "", fmt.Errorf("screen: invalid segment %q", segment)
	}
	encoded, err := Segment(name)
	if err != nil || encoded != segment {
		return "", fmt.Errorf("screen: noncanonical segment %q", segment)
	}
	return name, nil
}

// ParseView rejects zero, malformed and noncanonical references.
func ParseView(value string) (ViewRef, error) {
	p := strings.Split(value, "/")
	if len(p) < 4 || (p[0] != "app" && p[0] != "ops" && p[0] != "public") {
		return "", fmt.Errorf("screen: invalid view %q", value)
	}
	valid := len(p) == 4 && p[2] == "custom"
	if len(p) == 5 && p[2] == "resource" {
		valid = p[4] == "list" || p[4] == "detail" || p[4] == "create" || p[4] == "edit"
	}
	valid = valid || len(p) == 6 && p[2] == "resource" && p[4] == "command"
	if !valid {
		return "", fmt.Errorf("screen: invalid view %q", value)
	}
	for _, part := range p {
		if _, err := decode(part); err != nil {
			return "", err
		}
	}
	return ViewRef(value), nil
}

// Generic names a resource view, or a command when kind is command.
func Generic(surface, owner, entity, kind, verb string) (ViewRef, error) {
	o, err := Segment(owner)
	if err != nil {
		return "", err
	}
	e, err := Segment(entity)
	if err != nil {
		return "", err
	}
	value := surface + "/" + o + "/resource/" + e + "/" + kind
	if kind == "command" {
		v, err := Segment(verb)
		if err != nil {
			return "", err
		}
		value += "/" + v
	} else if verb != "" {
		return "", fmt.Errorf("screen: only a command has a verb")
	}
	return ParseView(value)
}

// Custom names a module-owned page, independently of its URL namespace.
func Custom(surface, owner, name string) (ViewRef, error) {
	o, err := Segment(owner)
	if err != nil {
		return "", err
	}
	n, err := Segment(name)
	if err != nil {
		return "", err
	}
	return ParseView(surface + "/" + o + "/custom/" + n)
}

func anchorParts(name string) ([]string, error) {
	p := strings.Split(name, ".")
	if len(p) < 2 || (p[0] != "field" && p[0] != "action" && p[0] != "command") || p[0] != "field" && len(p) != 2 {
		return nil, fmt.Errorf("screen: invalid anchor name %q", name)
	}
	for _, part := range p[1:] {
		if _, err := decode(part); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Anchor names a role and one or more source path components in this view.
// Dots inside a component are encoded and remain distinct from path separators.
func (v ViewRef) Anchor(role string, path ...string) (AnchorRef, error) {
	parts := []string{role}
	for _, name := range path {
		part, err := Segment(name)
		if err != nil {
			return "", err
		}
		parts = append(parts, part)
	}
	return ParseAnchor(string(v) + "#" + strings.Join(parts, "."))
}

// ParseAnchor rejects references without a valid view and role-namespaced name.
func ParseAnchor(value string) (AnchorRef, error) {
	view, name, ok := strings.Cut(value, "#")
	if !ok {
		return "", fmt.Errorf("screen: invalid anchor %q", value)
	}
	if _, err := ParseView(view); err != nil {
		return "", err
	}
	if _, err := anchorParts(name); err != nil {
		return "", err
	}
	return AnchorRef(value), nil
}

// Owner returns the decoded declaring module; an invalid reference returns empty.
func (v ViewRef) Owner() string {
	if _, err := ParseView(string(v)); err != nil {
		return ""
	}
	owner, _ := decode(strings.Split(string(v), "/")[1])
	return owner
}

// Kind returns list, detail, create, edit, command or custom, or empty if invalid.
func (v ViewRef) Kind() string {
	if _, err := ParseView(string(v)); err != nil {
		return ""
	}
	p := strings.Split(string(v), "/")
	if p[2] == "custom" {
		return "custom"
	}
	return p[4]
}
