package pkit

import (
	"fmt"
	"reflect"
	"strings"
)

// fixFor names the fix a missing provider needs: the module that owns the
// contract's package, off the house-rule-3 shape …/<module>/contracts.
func (a *App) fixFor(key reflect.Type) string {
	dir, pkg := owner(key)
	if pkg == "contracts" && dir != "" {
		return fmt.Sprintf("add %s.Module to %s", dir, a.name)
	}
	return "add a module that provides it"
}

func suppliersOf(lives []*Module, key reflect.Type) []*Module {
	var out []*Module
	for _, m := range lives {
		for _, dl := range m.decls {
			if (dl.kind == provides || dl.kind == contributes) && dl.key == key {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

func declaredKeys(lives []*Module) []reflect.Type {
	var out []reflect.Type
	seen := map[reflect.Type]bool{}
	for _, m := range lives {
		for _, dl := range m.decls {
			if (dl.kind == provides || dl.kind == contributes) && !seen[dl.key] {
				seen[dl.key] = true
				out = append(out, dl.key)
			}
		}
	}
	return out
}

func hasDecl(m *Module, k kind) bool {
	for _, dl := range m.decls {
		if dl.kind == k {
			return true
		}
	}
	return false
}

// hasDeclKey asks whether the module says this kind about this contract at all.
func hasDeclKey(m *Module, k kind, key reflect.Type) bool {
	for _, dl := range m.decls {
		if dl.kind == k && dl.key == key {
			return true
		}
	}
	return false
}

func survivingOf(cand []*Module, dead map[*Module]bool) []*Module {
	var out []*Module
	for _, m := range cand {
		if !dead[m] {
			out = append(out, m)
		}
	}
	return out
}

func allContributors(lives, cand []*Module) bool {
	for _, m := range cand {
		if !hasDeclKind(m, contributes) {
			return false
		}
	}
	return true
}

func hasDeclKind(m *Module, k kind) bool {
	for _, dl := range m.decls {
		if dl.kind == k {
			return true
		}
	}
	return false
}

func contributorsSay(lives, cand []*Module) string {
	if len(cand) == 2 {
		return fmt.Sprintf("%s and %s both contribute one", cand[0].name, cand[1].name)
	}
	return andList(moduleNames(cand)) + " contribute one each"
}

func moduleNames(ms []*Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.name + ".Module"
	}
	return out
}

func names(ms []*Module) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.name
	}
	return out
}

// whoOrNobody answers the sentence that has to say who is on the other end:
// the names, or the plain words that no module is. Explain's text is committed
// as the client's composition file, so an empty list has to read as one.
func whoOrNobody(ss []string) string {
	if len(ss) == 0 {
		return "no module"
	}
	return andList(ss)
}

func andList(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " and " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + " and " + ss[len(ss)-1]
	}
}

func contains(ms []*Module, m *Module) bool {
	for _, x := range ms {
		if x == m {
			return true
		}
	}
	return false
}

func filter(ms []*Module, keep func(*Module) bool) []*Module {
	var out []*Module
	for _, m := range ms {
		if keep(m) {
			out = append(out, m)
		}
	}
	return out
}
