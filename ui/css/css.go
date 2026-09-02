// Package css is the CSS intermediate representation the stylesheet is built
// in: typed values, declarations, rules, custom properties and the two
// at-rules the design system uses.
//
// It exists so that the application's stylesheet is a Go value rather than a
// file somebody edits. ui/style compiles a component's class list into rules
// here; design writes its tokens here as custom properties; the result renders
// once at startup and is served as one artifact. Nothing parses CSS, because
// nothing in this repository authors any.
//
// Derived from github.com/septagon-oss/styleengine (Apache-2.0); see NOTICE.
// The parser, the minifier, the diagnostics and the @layer/@supports/@font-face
// families were left behind with the repository: this package emits what one
// design system needs and reads nothing.
package css

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Value is the right-hand side of a declaration. A Value renders itself, so a
// var() reference cannot be confused with the text of one.
type Value interface {
	CSS() string
	isValue()
}

type literalValue string

func (v literalValue) CSS() string { return string(v) }
func (literalValue) isValue()      {}

// Literal is a value emitted verbatim. Every caller in this repository passes a
// token value or a string it built itself; there is no untrusted input here,
// because nobody outside the binary contributes CSS.
func Literal(s string) Value { return literalValue(s) }

type varRefValue struct {
	name     string
	fallback string
}

func (v varRefValue) CSS() string {
	if v.fallback == "" {
		return "var(--" + v.name + ")"
	}
	return "var(--" + v.name + ", " + v.fallback + ")"
}
func (varRefValue) isValue() {}

// VarRef references a custom property. The leading "--" is added here, so the
// name a theme registers and the name a rule reads are the same string.
//
// It panics on a name or a fallback that could break out of the var()
// expression. This is a wiring mistake in Go source, like httpx.Permission's:
// the alternative is a stylesheet that renders once, silently malformed.
func VarRef(name, fallback string) Value {
	if !validVarName(name) {
		panic(fmt.Sprintf("css: invalid var name %q (want [a-z][a-z0-9_-]*)", name))
	}
	if strings.ContainsAny(fallback, ")(;{}") || strings.Contains(fallback, "/*") {
		panic(fmt.Sprintf("css: invalid var fallback for %q: %q", name, fallback))
	}
	return varRefValue{name: name, fallback: fallback}
}

var varNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validVarName(s string) bool { return varNameRE.MatchString(s) }

// Declaration is one property and its value.
type Declaration struct {
	Property string
	Value    Value
}

// Decl builds a Declaration.
func Decl(property string, value Value) Declaration {
	return Declaration{Property: property, Value: value}
}

// CSS renders the declaration without its terminator.
func (d Declaration) CSS() string { return d.Property + ": " + d.Value.CSS() }

// Rule is a selector and what it declares. The selector is a string rather than
// a parsed type: every selector in this repository is built by ui/style from a
// class name it just compiled, so there is nothing to normalise and nothing a
// caller could get wrong that a parser would catch.
type Rule struct {
	Selector string
	Decls    []Declaration
}

// CSS renders the rule, custom properties first and alphabetically so that a
// theme block diffs stably, everything else in the order it was declared.
func (r Rule) CSS() string {
	decls := append([]Declaration(nil), r.Decls...)
	sort.SliceStable(decls, func(i, j int) bool {
		iv, jv := strings.HasPrefix(decls[i].Property, "--"), strings.HasPrefix(decls[j].Property, "--")
		if iv != jv {
			return iv
		}
		return iv && jv && decls[i].Property < decls[j].Property
	})
	var b strings.Builder
	b.WriteString(r.Selector)
	b.WriteString(" {")
	for _, d := range decls {
		b.WriteString("\n  ")
		b.WriteString(d.CSS())
		b.WriteByte(';')
	}
	b.WriteString("\n}")
	return b.String()
}

// Sheet is an ordered, deduplicated collection of rules and at-rules. It is
// built by one goroutine and then rendered; it carries no lock, because the
// application builds its stylesheet once.
type Sheet struct {
	rules   []*Rule
	index   map[string]int
	atRules []atRule
}

// NewSheet returns an empty Sheet.
func NewSheet() *Sheet { return &Sheet{index: map[string]int{}} }

// AddRule adds a rule, merging into an existing one with the same selector so
// that two components contributing to one selector do not produce two blocks.
func (s *Sheet) AddRule(r Rule) *Sheet {
	if i, ok := s.index[r.Selector]; ok {
		s.rules[i].Decls = merge(s.rules[i].Decls, r.Decls)
		return s
	}
	cp := r
	s.rules = append(s.rules, &cp)
	s.index[r.Selector] = len(s.rules) - 1
	return s
}

// merge is last-write-wins per property.
func merge(into, from []Declaration) []Declaration {
	at := map[string]int{}
	for i, d := range into {
		at[d.Property] = i
	}
	for _, d := range from {
		if i, ok := at[d.Property]; ok {
			into[i] = d
			continue
		}
		at[d.Property] = len(into)
		into = append(into, d)
	}
	return into
}

// Var declares a custom property in :root. Re-declaring one replaces it.
func (s *Sheet) Var(name, value string) *Sheet {
	if !validVarName(name) {
		panic(fmt.Sprintf("css: invalid var name %q", name))
	}
	return s.AddRule(Rule{Selector: ":root", Decls: []Declaration{Decl("--"+name, Literal(value))}})
}

// Select declares a rule on an arbitrary selector, which is how a theme block
// ([data-theme="dark"]) declares the same properties as :root.
func (s *Sheet) Select(selector string, decls ...Declaration) *Sheet {
	return s.AddRule(Rule{Selector: selector, Decls: decls})
}

// Merge appends another sheet's rules and at-rules into this one.
func (s *Sheet) Merge(other *Sheet) *Sheet {
	if s == nil || other == nil {
		return s
	}
	for _, r := range other.rules {
		s.AddRule(Rule{Selector: r.Selector, Decls: append([]Declaration(nil), r.Decls...)})
	}
	s.atRules = append(s.atRules, other.atRules...)
	return s
}

// Rules is every top-level rule, in insertion order. It is read by the test
// that proves every class a component declares resolves to something.
func (s *Sheet) Rules() []Rule {
	if s == nil {
		return nil
	}
	out := make([]Rule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, *r)
	}
	return out
}

// atRule is @media or @keyframes: the two this design system uses. A third
// family arrives with the first rule that needs one.
type atRule struct {
	prelude string
	inner   *Sheet
	stops   []stop
}

type stop struct {
	offset string
	decls  []Declaration
}

// Media nests a @media block.
func (s *Sheet) Media(query string, fn func(*Sheet)) *Sheet {
	inner := NewSheet()
	fn(inner)
	s.atRules = append(s.atRules, atRule{prelude: "@media " + query, inner: inner})
	return s
}

// Keyframes declares an animation. Stops render in offset order, so "from" and
// "0%" are the same place whichever a caller wrote.
func (s *Sheet) Keyframes(name string, fn func(*Keyframes)) *Sheet {
	k := &Keyframes{}
	fn(k)
	s.atRules = append(s.atRules, atRule{prelude: "@keyframes " + name, stops: k.stops})
	return s
}

// Keyframes accumulates the stops of one animation.
type Keyframes struct{ stops []stop }

// At adds one stop: "from", "to", or a percentage.
func (k *Keyframes) At(offset string, decls ...Declaration) *Keyframes {
	k.stops = append(k.stops, stop{offset: offset, decls: decls})
	return k
}

// CSS renders the whole sheet: rules first, at-rules after, each separated by a
// blank line. The output is deterministic, which is what makes the stylesheet
// something a test can assert about.
func (s *Sheet) CSS() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	for i, r := range s.rules {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(r.CSS())
	}
	for _, a := range s.atRules {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(a.CSS())
	}
	return b.String()
}

func (a atRule) CSS() string {
	if a.inner != nil {
		body := a.inner.CSS()
		if body == "" {
			return a.prelude + " {\n}"
		}
		return a.prelude + " {\n" + indent(body) + "\n}"
	}
	stops := append([]stop(nil), a.stops...)
	sort.SliceStable(stops, func(i, j int) bool { return order(stops[i].offset) < order(stops[j].offset) })
	var b strings.Builder
	b.WriteString(a.prelude)
	b.WriteString(" {")
	for _, k := range stops {
		b.WriteString("\n  ")
		b.WriteString(k.offset)
		b.WriteString(" {")
		for _, d := range k.decls {
			b.WriteString("\n    ")
			b.WriteString(d.CSS())
			b.WriteByte(';')
		}
		b.WriteString("\n  }")
	}
	b.WriteString("\n}")
	return b.String()
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n")
}

// order sorts keyframe offsets: from is 0, to is 100, "n%" is n.
func order(offset string) int {
	switch offset {
	case "from":
		return 0
	case "to":
		return 100
	}
	n := 0
	for _, r := range strings.TrimSuffix(offset, "%") {
		if r < '0' || r > '9' {
			return 1000
		}
		n = n*10 + int(r-'0')
	}
	return n
}
