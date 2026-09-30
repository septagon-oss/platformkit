// Package css is the CSS intermediate representation the stylesheet is built
// in: typed values, declarations, rules, custom properties and the cascade
// layers and at-rules the design system uses.
//
// It exists so that the application's stylesheet is a Go value rather than a
// file somebody edits. ui/style compiles a component's class list into rules
// here; design writes its tokens here as custom properties; the result renders
// once at startup and is served as one artifact. Nothing parses CSS, because
// nothing in this repository authors any.
//
// Derived from github.com/septagon-oss/styleengine (Apache-2.0); see NOTICE —
// by attribution only: this package takes no upstream code, and the upstream
// emits @layer, @supports and @font-face, so the removal the fork is remembered
// for is upstream's, not this tree's. The parser, the minifier and the
// diagnostics were left behind with the repository, and with them @supports and
// @font-face, which nothing here needs: this package emits what one design
// system needs and reads nothing.
// Cascade layers returned because precedence by file order is not a promise:
// Layer and LayerOrder emit them, and ui.Compose is what places a consumer's
// rules in the layer a consumer may write.
package css

import (
	"fmt"
	"regexp"
	"slices"
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

// Sheet keeps top-level rules in contribution order, followed by at-rules.
// Only adjacent equal selectors share a block; declarations retain CSS cascade
// order. It is built by one goroutine and carries no lock.
type Sheet struct {
	rules   []*Rule
	atRules []atRule
	orders  []string
}

// NewSheet returns an empty Sheet.
func NewSheet() *Sheet { return new(Sheet) }

// AddRule copies a contribution without moving it across another selector.
// Combining nonadjacent rules changes equal-specificity precedence. Removing
// repeated declarations also loses !important, fallbacks and shorthand order.
func (s *Sheet) AddRule(r Rule) *Sheet {
	if i := len(s.rules) - 1; i >= 0 && s.rules[i].Selector == r.Selector {
		s.rules[i].Decls = append(s.rules[i].Decls, r.Decls...)
		return s
	}
	cp := r
	cp.Decls = slices.Clone(r.Decls)
	s.rules = append(s.rules, &cp)
	return s
}

// Var contributes a custom property to :root under the ordinary CSS cascade.
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
		s.AddRule(*r)
	}
	s.atRules = append(s.atRules, other.atRules...)
	s.orders = append(s.orders, other.orders...)
	return s
}

// WalkRules visits every rule's selector and declarations in cascade order,
// including the rules nested in at-rules, so a caller that must read a sheet
// reads what a browser would apply and not just the top level. A keyframe stop
// is such a rule: the browser applies its declarations to the animating element,
// so the walk reports it with its offset ("from", "50%") where a selector goes.
// Returning the error from fn stops the walk.
func (s *Sheet) WalkRules(fn func(selector string, decls []Declaration) error) error {
	if s == nil {
		return nil
	}
	for _, r := range s.rules {
		if err := fn(r.Selector, r.Decls); err != nil {
			return err
		}
	}
	for _, a := range s.atRules {
		if err := a.inner.WalkRules(fn); err != nil {
			return err
		}
		for _, st := range a.stops {
			if err := fn(st.offset, st.decls); err != nil {
				return err
			}
		}
	}
	return nil
}

// Verbatim visits every string the sheet writes into its output unchanged: each
// selector, each declaration property, each rendered value, each keyframe offset
// and each at-rule prelude, at any depth, in emission order.
//
// It exists because the emitter adds the block boundaries and the declaration
// terminators *around* those strings and nothing inside them: a `{` arriving in
// one is not text to a browser but the start of a block, and a `}` the end of
// one, so the bytes after them belong to whatever block the browser is then
// reading rather than to the rule they came from. A caller that must know what a
// browser reads — ui.Compose, before it places a consumer's rules inside a layer
// it wrote — asks for this instead of trusting the fields it was handed.
func (s *Sheet) Verbatim(fn func(text string) error) error {
	if s == nil {
		return nil
	}
	for _, r := range s.rules {
		if err := verbatimDecls(fn, r.Selector, r.Decls); err != nil {
			return err
		}
	}
	for _, a := range s.atRules {
		if err := fn(a.prelude); err != nil {
			return err
		}
		if err := a.inner.Verbatim(fn); err != nil {
			return err
		}
		for _, st := range a.stops {
			if err := verbatimDecls(fn, st.offset, st.decls); err != nil {
				return err
			}
		}
	}
	return nil
}

func verbatimDecls(fn func(text string) error, head string, decls []Declaration) error {
	if err := fn(head); err != nil {
		return err
	}
	for _, d := range decls {
		if err := fn(d.Property); err != nil {
			return err
		}
		if err := fn(d.Value.CSS()); err != nil {
			return err
		}
	}
	return nil
}

// Heads visits every text the sheet writes ahead of a `{` the emitter supplies:
// each selector, each keyframe offset and each at-rule head — the query or name
// the caller passed, with the at-keyword the emitter prefixed stepped past, since
// that keyword is the emitter's and the text a caller is asked about is not. At
// any depth, in emission order.
//
// The position, not the string, decides what a browser reads there: a `;` is the
// end of the rule the emitter is writing, so what follows it is the head of the
// next rule, and an `@` is the keyword of the at-rule the block then belongs to.
// Verbatim visits these strings plus the declarations between the braces, where a
// `;` is data a url() carries; a caller that must police the position ahead of the
// brace asks for this rather than for the fields it was handed.
func (s *Sheet) Heads(fn func(text string) error) error {
	if s == nil {
		return nil
	}
	for _, r := range s.rules {
		if err := fn(r.Selector); err != nil {
			return err
		}
	}
	for _, a := range s.atRules {
		_, head, _ := strings.Cut(a.prelude, " ") // the emitter prefixes the keyword:
		// "@media all" reaches the caller as the query it was handed, "all"
		if err := fn(head); err != nil {
			return err
		}
		if err := a.inner.Heads(fn); err != nil {
			return err
		}
		for _, st := range a.stops {
			if err := fn(st.offset); err != nil {
				return err
			}
		}
	}
	return nil
}

// UsesLayers reports whether the sheet emits @layer — an order statement or a
// block — at any depth.
func (s *Sheet) UsesLayers() bool {
	if s == nil {
		return false
	}
	if len(s.orders) > 0 {
		return true
	}
	for _, a := range s.atRules {
		if a.layer || a.inner.UsesLayers() {
			return true
		}
	}
	return false
}

// Rules returns detached top-level rules and declarations in insertion order.
func (s *Sheet) Rules() []Rule {
	if s == nil {
		return nil
	}
	out := make([]Rule, 0, len(s.rules))
	for _, r := range s.rules {
		out = append(out, Rule{Selector: r.Selector, Decls: slices.Clone(r.Decls)})
	}
	return out
}

// atRule is @media or @keyframes: the two this design system uses. A third
// family arrives with the first rule that needs one.
type atRule struct {
	prelude string
	inner   *Sheet
	stops   []stop
	layer   bool
}

var layerNameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// LayerOrder declares the cascade order of the named layers, emitted as one
// `@layer a, b;` statement ahead of every rule. The statement is what makes
// precedence a promise rather than a file order: within one origin a rule in
// an earlier layer loses to a later one whatever its selector says.
// It panics on a name that is not a lower-case custom identifier for the same
// reason VarRef does: layer names are written once in Compose, so a malformed
// one is a wiring mistake, not input.
func (s *Sheet) LayerOrder(names ...string) *Sheet {
	for _, n := range names {
		if !layerNameRE.MatchString(n) {
			panic(fmt.Sprintf("css: invalid layer name %q (want [a-z][a-z0-9_-]*)", n))
		}
	}
	s.orders = append(s.orders, strings.Join(names, ", "))
	return s
}

// Layer nests a @layer block. The sheet built inside the block stays inside
// it; only the declared order decides whether it beats another layer.
func (s *Sheet) Layer(name string, fn func(*Sheet)) *Sheet {
	if !layerNameRE.MatchString(name) {
		panic(fmt.Sprintf("css: invalid layer name %q (want [a-z][a-z0-9_-]*)", name))
	}
	inner := NewSheet()
	fn(inner)
	s.atRules = append(s.atRules, atRule{prelude: "@layer " + name, inner: inner, layer: true})
	return s
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

// CSS renders the whole sheet: layer statements first (they precede the rules
// they order), rules next, other at-rules after, each separated by a blank
// line. The output is deterministic, which is what makes the stylesheet
// something a test can assert about.
func (s *Sheet) CSS() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	for _, o := range s.orders {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("@layer ")
		b.WriteString(o)
		b.WriteByte(';')
	}
	for _, r := range s.rules {
		if b.Len() > 0 {
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
