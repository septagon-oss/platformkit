package pkit

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// dependsOnLast refuses each way one module can be placed after the module
// that runs after everything: it needs or uses what that module provides, or
// it takes every contribution of what that module contributes. Whoever takes a
// contribution is built after every module that contributes it, so the two
// 0074 rows — “runs after every other module” and “many contribute to it” —
// meet in one composition only as a refusal.
func (a *App) dependsOnLast(p *plan, fail func(string, string, ...any), m *Module) bool {
	died := false
	for _, dl := range m.decls {
		if dl.kind != needs && dl.kind != optional {
			continue
		}
		if dl.many {
			if hasDeclKey(p.last, contributes, dl.key) {
				fail("Use", "%s takes every %s, which %s contributes after everything: nothing may take a contribution from a module that runs after everything",
					m.name, contract(dl.key), p.last.name)
				died = true
			}
			continue
		}
		if p.from[edge{m, dl.key}] == p.last {
			verb := "needs"
			if dl.kind == optional {
				verb = "uses"
			}
			fail("Use", "%s %s %s, which %s provides after everything: nothing may need a module that runs after everything",
				m.name, verb, contract(dl.key), p.last.name)
			died = true
		}
	}
	return died
}

// pickImplementation answers a module's FromDeployment for a deployment whose
// environment resolve has already accepted.
func (a *App) pickImplementation(p *plan, fail func(string, string, ...any), m *Module, dl Declaration, d Deployment) {
	// A module that names no implementation at all has nothing to pick; the
	// sentence below would blame a simulation that is not declared.
	if len(dl.impls) == 0 {
		fail("Deployment", "%s declares FromDeployment with no implementation to pick: name one, or do not declare it", m.name)
		return
	}
	// In development the first implementation whose inputs are all set, and
	// failing that the simulated one; anywhere else only a real
	// implementation, and never one whose inputs are missing.
	wanted := func(i Implementation) bool { return !i.Simulated }
	if d.Environment == Development {
		wanted = func(Implementation) bool { return true }
	}
	firstMissing := ""
	for _, impl := range dl.impls {
		if !wanted(impl) {
			continue
		}
		missing := ""
		for _, in := range impl.Inputs {
			if d.Inputs[in] == "" {
				missing = in
				break
			}
		}
		if missing == "" {
			p.impl[m] = impl.Name
			return
		}
		if firstMissing == "" {
			firstMissing = missing
		}
	}
	if firstMissing != "" {
		fail("Deployment", "%s needs %s in %s: set it, or the deployment cannot pick %s", m.name, firstMissing, d.Environment, m.name)
		return
	}
	fail("Deployment", "%s has no implementation to run in %s: every implementation it declares only simulates %s",
		m.name, d.Environment, m.name)
}

// order places every living module after the modules it needs and the ones
// it takes contributions from, Use order breaking ties. It returns the first
// cycle the graph has, and — when the graph has no cycle but still cannot
// place a module — the modules it could not place: a composition is never
// answerable by leaving a module somebody named in Use out of it in silence.
func (a *App) order(lives []*Module, dead map[*Module]bool, p *plan) ([]*Module, []*Module, []*Module) {
	deps := map[*Module][]*Module{}
	var queue []*Module
	for _, m := range lives {
		if dead[m] {
			continue
		}
		d := map[*Module]bool{}
		for _, dl := range m.decls {
			switch {
			case dl.kind == needs && !dl.many, dl.kind == optional && !dl.many:
				if s := p.from[edge{m, dl.key}]; s != nil && s != m {
					d[s] = true
				}
			case (dl.kind == needs || dl.kind == optional) && dl.many:
				for _, c := range survivingOf(p.contributors[dl.key], dead) {
					if c != m {
						d[c] = true
					}
				}
			}
		}
		var list []*Module
		for _, c := range lives {
			if d[c] {
				list = append(list, c)
			}
		}
		deps[m] = list
		p.deps[m] = list
		if m != p.last && len(list) == 0 {
			queue = append(queue, m)
		}
	}
	var out []*Module
	placed := map[*Module]bool{}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		if placed[m] {
			continue
		}
		ready := true
		for _, dep := range deps[m] {
			if !placed[dep] {
				ready = false
				break
			}
		}
		if !ready {
			continue
		}
		placed[m] = true
		out = append(out, m)
		for _, c := range lives {
			if dead[c] || placed[c] || c == p.last || !contains(deps[c], m) {
				continue
			}
			ready := true
			for _, dep := range deps[c] {
				if !placed[dep] {
					ready = false
					break
				}
			}
			if ready {
				queue = append(queue, c)
			}
		}
	}
	if p.last != nil && !dead[p.last] {
		placed[p.last] = true
		out = append(out, p.last)
	}
	var left []*Module
	for _, m := range lives {
		if !dead[m] && !placed[m] {
			left = append(left, m)
		}
	}
	if len(left) == 0 {
		return out, nil, nil
	}
	if cycle := cycleThrough(deps, left[0]); cycle != nil {
		return out, cycle, nil
	}
	return out, nil, left
}

// cycleThrough walks dependency edges from the first unplaced module in Use
// order and returns the modules of the first cycle it closes, head repeated
// at the tail: cart → collectibles → cart.
func cycleThrough(deps map[*Module][]*Module, from *Module) []*Module {
	state := map[*Module]int{} // 0 unvisited, 1 on the walk, 2 done
	var stack []*Module
	var walk func(*Module) []*Module
	walk = func(m *Module) []*Module {
		state[m] = 1
		stack = append(stack, m)
		for _, dep := range deps[m] {
			switch state[dep] {
			case 1:
				si := slices.Index(stack, dep)
				return append(slices.Clone(stack[si:]), dep)
			case 0:
				if c := walk(dep); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[m] = 2
		return nil
	}
	return walk(from)
}

// cycleReasons explains each selected dependency in cycle order, preserving
// declaration order within an edge and saying identical reasons only once.
func (p *plan) cycleReasons(cycle []*Module) string {
	var edges []string
	for i, consumer := range cycle[:len(cycle)-1] {
		supplier := cycle[i+1]
		var reasons []string
		for _, dl := range consumer.decls {
			if dl.kind != needs && dl.kind != optional {
				continue
			}
			verb := "needs"
			if dl.many {
				if !slices.Contains(p.contributors[dl.key], supplier) {
					continue
				}
				verb = "takes"
			} else {
				if p.from[edge{consumer, dl.key}] != supplier {
					continue
				}
				if dl.kind == optional {
					verb = "uses"
				}
			}
			reason := fmt.Sprintf("%s %s %s from %s", consumer.name, verb, contract(dl.key), supplier.name)
			if !slices.Contains(reasons, reason) {
				reasons = append(reasons, reason)
			}
		}
		edges = append(edges, strings.Join(reasons, ", "))
	}
	return strings.Join(edges, "; ")
}

// firstTaker is the module in Use order that takes one T: the one whose
// sentence names it.
func firstTaker(lives []*Module, key reflect.Type) string {
	for _, m := range lives {
		for _, dl := range m.decls {
			if dl.kind == needs && !dl.many && dl.key == key {
				return m.name
			}
		}
	}
	return ""
}
