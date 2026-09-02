package style

// This half of the package renders the CSS for the classes the builder
// compiles. The stylesheet is three layers:
//
//	:root { --pk-color-* }  the theme's tokens, from package design
//	:root { --pk-role-* }   RoleVars(): the semantic roles below, in terms of
//	                        those tokens (or a color-mix derivation of them)
//	utility rules           For(): a rule for each class the components declare
//
// Nothing scans source and nothing runs node. For takes the class lists the
// components declare and emits exactly their rules, which is why a component
// that is deleted takes its CSS with it.
//
// Escape hatches stay escape hatches: a Raw class is deliberately
// unresolvable — the caller owns that CSS, and a typo fails closed here
// instead of emitting a rule that is wrong.

import (
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/ui/css"
)

// RoleVars emits the :root block mapping every tw color role to the design
// system's token variables, plus the keyframes the animate-* utilities
// reference. Serve it after the theme's token block and before the utilities.
func RoleVars() *css.Sheet {
	s := css.NewSheet()
	roles := roleValues()
	names := make([]string, 0, len(roles))
	for c := range roles {
		names = append(names, string(c))
	}
	sort.Strings(names)
	for _, n := range names {
		s.Var("pk-role-"+n, roles[Color(n)])
	}
	s.Keyframes("pk-spin", func(kb *css.Keyframes) {
		kb.At("from", css.Decl("transform", css.Literal("rotate(0deg)")))
		kb.At("to", css.Decl("transform", css.Literal("rotate(360deg)")))
	})
	s.Keyframes("pk-pulse", func(kb *css.Keyframes) {
		kb.At("0%, 100%", css.Decl("opacity", css.Literal("1")))
		kb.At("50%", css.Decl("opacity", css.Literal("0.5")))
	})
	return s
}

// For emits the rules for exactly the classes the given lists compile to,
// prefixed variants included. This is the deterministic, Go-native
// alternative to source scanning: components declare their ClassLists, and
// the stylesheet is derived from those declarations.
func For(lists ...ClassList) (*css.Sheet, error) {
	seen := map[string]bool{}
	var classes []string
	for _, l := range lists {
		for _, class := range strings.Fields(l.Compile()) {
			if !seen[class] {
				seen[class] = true
				classes = append(classes, class)
			}
		}
	}
	sort.Strings(classes)
	return Rules(classes...)
}
