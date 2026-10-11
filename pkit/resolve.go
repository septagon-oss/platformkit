package pkit

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
)

// edge is one module's single need for one contract: the pair the resolver
// answers with the module that supplies it.
type edge struct {
	to  *Module
	key reflect.Type
}

// declID is one thing a module says about itself, as the resolver tells it
// from every other thing that module says: the same kind about the same
// contract twice is the same statement twice.
type declID struct {
	kind kind
	key  reflect.Type
	many bool
}

// contractOf says one declaration in the module's own words, for the sentence
// that refuses to read it twice.
func contractOf(dl Declaration) string {
	noun := contract(dl.key)
	switch {
	case dl.kind == needs && dl.many:
		return "takes every " + noun
	case dl.kind == optional && dl.many:
		return "uses every " + noun
	case dl.kind == optional:
		return "uses " + noun
	}
	return noun
}

// plan is the resolved graph: who is built and in what order, who supplies
// whose need, whose contributions whom they take, which implementation the
// deployment picked, and what the app chose where two composed modules
// offered one contract.
type plan struct {
	order        []*Module
	deps         map[*Module][]*Module      // each living module's build dependencies, in Use order
	from         map[edge]*Module           // supplier of each settled single need
	contributors map[reflect.Type][]*Module // living modules that Contributes[T]
	takers       map[reflect.Type][]*Module // living modules that take T, one or every
	impl         map[*Module]string         // the implementation FromDeployment picked
	manifest     map[*Module]module.Module  // what each built module describes to the kernel
	choices      []choice                   // what Choose settled
	last         *Module                    // the module that runs after everything
	cfg          config.Config              // the deployment's configuration, read by Config
	reads        map[*Module][]reflect.Type // which section each built module asked Config for
}

type choice struct {
	key    reflect.Type
	picked *Module
	passed []string
}

// issue is one problem with the composition and the method that caused it.
// Build answers every issue at once, which is why resolving collects them
// instead of returning the first.
type issue struct {
	method string
	err    error
}

// resolve answers the declared app for one deployment. Every sentence it
// produces names the module, the contract and the fix; a follow-on error
// caused by a module that could not be resolved is suppressed, so one
// missing provider is one sentence.
func (a *App) resolve(d Deployment) (*plan, []issue) {
	var issues []issue
	fail := func(method, format string, args ...any) {
		issues = append(issues, issue{method: method, err: fmt.Errorf(format, args...)})
	}
	said := map[string]bool{}
	failOnce := func(id, method, format string, args ...any) {
		if said[id] {
			return
		}
		said[id] = true
		fail(method, format, args...)
	}

	p := &plan{
		from:         map[edge]*Module{},
		deps:         map[*Module][]*Module{},
		contributors: map[reflect.Type][]*Module{},
		takers:       map[reflect.Type][]*Module{},
		impl:         map[*Module]string{},
		manifest:     map[*Module]module.Module{},
		cfg:          d.Config,
		reads:        map[*Module][]reflect.Type{},
	}

	// The environment is the deployment's, not each module's: it is checked
	// once, where the deployment is read, not on the way to picking one
	// implementation. Checked only there, a composition with no
	// FromDeployment module would resolve, build and Explain under a name that
	// is not an environment — and Development is the only spelling that lets a
	// simulated implementation through, so "prod" for "production" is one
	// dropped letter that changes which rules the app is resolved under while
	// the answer that should name the mistake stays unsaid.
	envKnown := true
	switch d.Environment {
	case Development, Staging, Production:
	default:
		fail("Deployment", "%q is not an environment: name development, staging or production", string(d.Environment))
		envKnown = false
	}

	// Duplicates: one manifest named twice is one manifest built twice.
	var lives []*Module
	byName := map[string]*Module{}
	for _, m := range a.uses {
		if _, dup := byName[m.name]; dup {
			fail("Use", "%s is in Use twice: remove one", m.name)
			continue
		}
		byName[m.name] = m
		lives = append(lives, m)
	}

	// Choose may only name a module Use composed, and it must settle a
	// contract that has more than one supplier; a choice that picks the
	// only provider of everything chooses nothing.
	chosen := map[*Module]bool{}
	for _, c := range a.chosen {
		if !contains(lives, c) {
			fail("Choose", "Choose(%s.Module) names a module that is not in Use: add it to Use or remove the choice", c.name)
			continue
		}
		chosen[c] = true
		contested := false
		for _, dl := range c.decls {
			if (dl.kind == provides || dl.kind == contributes) && len(suppliersOf(lives, dl.key)) > 1 {
				contested = true
			}
		}
		if !contested {
			fail("Choose", "Choose(%s.Module) chooses nothing: no contract it provides has a second provider", c.name)
		}
	}

	// Where a contract has several suppliers, the chosen ones survive and
	// the rest are not built at all: the app picked, so the loser's module
	// is not in the composition. A contract nobody takes is never ambiguous
	// to anyone, so nothing waits to be chosen for it.
	survivingSuppliers := map[reflect.Type][]*Module{}
	// dead says a module is not built; refused says a refusal already says why it
	// is not, so the modules that needed it may stay silent. The two are not the
	// same set: Choose withdraws the module it did not pick without refusing it,
	// because a choice is not a failure, and a withdrawn module says nothing of
	// its own — which is why missingNeed below asks who it was chosen over.
	dead := map[*Module]bool{}
	refused := map[*Module]bool{}
	chosenOver := map[*Module]string{}
	for _, key := range declaredKeys(lives) {
		cand := suppliersOf(lives, key)
		picked := filter(cand, func(m *Module) bool { return chosen[m] })
		if len(picked) == 0 {
			survivingSuppliers[key] = cand
			continue
		}
		survivingSuppliers[key] = picked
		passed := names(filter(cand, func(m *Module) bool { return !chosen[m] }))
		for _, c := range picked {
			p.choices = append(p.choices, choice{key: key, picked: c, passed: passed})
		}
		if len(cand) > 1 {
			for _, m := range filter(cand, func(m *Module) bool { return !chosen[m] }) {
				dead[m] = true
				chosenOver[m] = andList(moduleNames(picked))
			}
		}
	}

	// What a module says about itself, it says once: the same declaration
	// twice would count it twice among the contributors to its contract, and
	// a module that needs the contract it supplies itself asks to be built
	// before itself, which no order can hold.
	for _, m := range lives {
		once := map[declID]bool{}
		for _, dl := range m.decls {
			switch dl.kind {
			case provides, needs, optional, contributes:
			default:
				continue
			}
			id := declID{kind: dl.kind, key: dl.key}
			if dl.kind == needs || dl.kind == optional {
				if dl.many && hasDeclKey(m, contributes, dl.key) {
					fail("Use", "%s takes every %s and contributes one itself: a module cannot be built before its own contribution",
						m.name, contract(dl.key))
					continue
				}
				if !dl.many && hasDeclKey(m, provides, dl.key) {
					fail("Use", "%s needs %s, which it provides itself: a module cannot be built before itself",
						m.name, contract(dl.key))
					continue
				}
				if !dl.many && hasDeclKey(m, contributes, dl.key) {
					fail("Use", "%s needs %s, which it contributes itself: a module cannot be built before its own contribution",
						m.name, contract(dl.key))
					continue
				}
				id.many = dl.many
			}
			if dl.kind == provides && hasDeclKey(m, contributes, dl.key) {
				fail("Use", "%s provides %s and contributes one %s: a contract is either the app's one provider or a contribution, not both in one module",
					m.name, contract(dl.key), contract(dl.key))
				continue
			}
			if once[id] {
				fail("Use", "%s declares it %s twice: a module says each thing once", m.name, contractOf(dl))
				continue
			}
			once[id] = true
		}
	}

	// One module may run after everything; the one that also asks is
	// refused before any of it means anything.
	var afters []*Module
	for _, m := range lives {
		if hasDecl(m, after) {
			afters = append(afters, m)
		}
	}
	if len(afters) > 1 {
		switch len(afters) {
		case 2:
			fail("Use", "%s and %s both ask to run after everything: only one module may", afters[0].name, afters[1].name)
		default:
			fail("Use", "%s ask to run after everything: only one module may", andList(names(afters)))
		}
		p.last = afters[0]
		for _, m := range afters[1:] {
			dead[m] = true
			refused[m] = true
		}
	} else if len(afters) == 1 {
		p.last = afters[0]
	}

	// One contract, one answer for the whole app. The sweep above refuses the
	// pair inside one module in the resolver's own words; the rule belongs to
	// the contract, so a second module saying the other half does not settle
	// it. And the resolver would then read the app two ways: Choose and a
	// single need count Provides and Contributes as suppliers of one contract,
	// while a module that takes every E reads contributions alone — so the
	// provided value is built, put, and belongs to nobody, with no sentence
	// about it. Read off what Use was given rather than what survived: Choose
	// is not the fix for this pair, because withdrawing one module of the pair
	// leaves the same value dropped with a choice in front of it.
	mixed := map[reflect.Type]bool{}
	for _, key := range declaredKeys(lives) {
		var providers, contributors []*Module
		for _, m := range suppliersOf(lives, key) {
			if hasDeclKey(m, provides, key) {
				providers = append(providers, m)
			}
			if hasDeclKey(m, contributes, key) {
				contributors = append(contributors, m)
			}
		}
		if len(providers) == 0 || len(contributors) == 0 ||
			(len(providers) == 1 && len(contributors) == 1 && providers[0] == contributors[0]) {
			continue
		}
		mixed[key] = true
		failOnce("mixed "+key.String(), "Use", "%s is provided by %s and contributed by %s: a contract is either the app's one provider or a contribution, not both in one app — make one of them the other; Choose cannot settle it",
			contract(key), andList(moduleNames(providers)), andList(moduleNames(contributors)))
	}

	// A hard need no living module answers is the sentence 0074 rule 3 names
	// first, and it is owed unless the composition already said why nothing is
	// left to answer it — which it has when every module that could has died
	// under a refusal that named it. A supplier the app chose away is no such
	// refusal: Choose withdraws a whole module, so a module that provides two
	// contracts takes the second one with it, and the module left beside the
	// winner still names a need only the loser answered. Left unsaid, the
	// withdrawal drops a module Use named, Validate answers nil, and the
	// composition file the client commits describes an app smaller than the one
	// it was given — so the withdrawal is named, with the fix that settles it.
	missingNeed := func(m *Module, dl Declaration) {
		sup := survivingSuppliers[dl.key]
		id := "missing " + m.name + " " + dl.key.String()
		if len(sup) == 0 {
			failOnce(id, "Use", "%s needs %s: %s", m.name, contract(dl.key), a.fixFor(dl.key))
			return
		}
		for _, s := range sup {
			if refused[s] {
				continue
			}
			failOnce(id, "Use", "%s needs %s: %s.Module provides it and %s chose %s over it — Choose %s.Module or compose a module that provides %s",
				m.name, contract(dl.key), s.name, a.name, chosenOver[s], s.name, contract(dl.key))
			return
		}
	}

	// Every need is resolved for every living module, the module that runs
	// after everything included: it is built last, so whatever it needs is
	// already placed when it is asked for. Skipping it is how its own need
	// went unanswered — built with the zero value, and no sentence about it.
	//
	// A need with one supplier is an edge; with none it is the missing
	// sentence, unless the supplier exists but could not itself be resolved,
	// which is the follow-on error that stays unsaid. With several suppliers it
	// is the ambiguity sentence, said once for the contract. Nothing changes
	// until the composition settles, so an unmet need withdraws its supplier's
	// takers too: a module whose settled supplier died under a refusal that
	// named it is not built either, and says nothing, because the composition
	// said that once already. An optional need of such a supplier simply goes
	// unfilled, the way it does when no provider is composed at all.
	//
	// Refused in the same sweep is any dependency on the module that runs
	// after everything, whichever way it is spelled. 0074's table carries two
	// rows no one module can both hold — "built after every other module" and
	// "many contribute to it, and the one is built after all of them":
	// whoever depends on the module that runs after everything would have to
	// be built after it, which no order holds.
	for {
		changed := false
		for _, m := range lives {
			if dead[m] {
				continue
			}
			for _, dl := range m.decls {
				if dl.many || (dl.kind != needs && dl.kind != optional) {
					continue
				}
				e := edge{m, dl.key}
				if s := p.from[e]; s != nil {
					if !dead[s] {
						continue
					}
					if dl.kind == needs {
						// Its supplier died, and only under a refusal: Choose withdraws
						// modules before this sweep runs, so nothing withdrawn is ever
						// wired in here. This module is not built and says nothing; that
						// refusal is the composition's one sentence.
						dead[m] = true
						refused[m] = true
					} else {
						p.from[e] = nil
					}
					changed = true
					break
				}
				cand := survivingOf(survivingSuppliers[dl.key], dead)
				switch len(cand) {
				case 1:
					p.from[e] = cand[0]
				case 0:
					if dl.kind == optional {
						p.from[e] = nil
						continue
					}
					missingNeed(m, dl)
					dead[m] = true
					refused[m] = true
					changed = true
				default:
					switch {
					case mixed[dl.key]:
						// The pair is refused once for the contract above; a
						// second sentence here would name Choose, which is not
						// the fix for it.
					case allContributors(lives, cand) && dl.kind == needs:
						failOnce("ambiguous "+dl.key.String(), "Choose", "%s takes one %s, and %s: Choose in %s",
							firstTaker(lives, dl.key), dl.key.Name(), contributorsSay(lives, cand), a.name)
					default:
						failOnce("ambiguous "+dl.key.String(), "Choose", "%s has %s: Choose one in %s",
							contract(dl.key), andList(moduleNames(cand)), a.name)
					}
					dead[m] = true
					refused[m] = true
					changed = true
				}
			}
			if !dead[m] && p.last != nil && m != p.last {
				if a.dependsOnLast(p, fail, m) {
					dead[m] = true
					refused[m] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	// What the deployment picks, it picks honestly: a simulated
	// implementation runs in development only, and a real one only with
	// the inputs it reads. An environment it does not know is refused above,
	// once, and nothing is picked under it: whichever implementation it named
	// would be a guess at which rules the deployment was resolved under.
	if envKnown {
		for _, m := range lives {
			if dead[m] {
				continue
			}
			for _, dl := range m.decls {
				if dl.kind == fromDeployment {
					a.pickImplementation(p, fail, m, dl, d)
				}
			}
		}
	}

	// Contributors and takers, read off the survivors.
	for _, m := range lives {
		if dead[m] {
			continue
		}
		for _, dl := range m.decls {
			switch {
			case dl.kind == contributes:
				p.contributors[dl.key] = append(p.contributors[dl.key], m)
			case dl.kind == needs || dl.kind == optional:
				p.takers[dl.key] = append(p.takers[dl.key], m)
			}
		}
	}

	// The order, the cycle that has no order, and any module no order places.
	order, cycle, unplaced := a.order(lives, dead, p)
	p.order = order
	if cycle != nil {
		fail("Use", "%s is a cycle", strings.Join(cycle, " → "))
	}
	for _, m := range unplaced {
		fail("Use", "%s is in Use but no build order places it: it waits for %s, which no order reaches before it",
			m.name, andList(moduleNames(p.deps[m])))
	}
	return p, issues
}

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
func (a *App) order(lives []*Module, dead map[*Module]bool, p *plan) ([]*Module, []string, []*Module) {
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
	if cycle := cycleThrough(lives, deps, left[0]); cycle != nil {
		return out, cycle, nil
	}
	return out, nil, left
}

// cycleThrough walks dependency edges from the first unplaced module in Use
// order and returns the names of the first cycle it closes, head repeated
// at the tail: cart → collectibles → cart.
func cycleThrough(lives []*Module, deps map[*Module][]*Module, from *Module) []string {
	state := map[*Module]int{} // 0 unvisited, 1 on the walk, 2 done
	var stack []*Module
	var namesOf []string
	var walk func(*Module) []string
	walk = func(m *Module) []string {
		state[m] = 1
		stack = append(stack, m)
		namesOf = append(namesOf, m.name)
		for _, dep := range deps[m] {
			switch state[dep] {
			case 1:
				for si, s := range stack {
					if s == dep {
						path := append([]string{}, namesOf[si:]...)
						return append(path, dep.name)
					}
				}
			case 0:
				if c := walk(dep); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		namesOf = namesOf[:len(namesOf)-1]
		state[m] = 2
		return nil
	}
	return walk(from)
}

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
