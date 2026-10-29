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
	cloud        cloudPlan                  // what the deployment's configuration answers about its seams
}

type choice struct {
	key    reflect.Type
	picked *Module
	passed []string
}

// issue is one problem with the composition and the method that caused it.
// Build answers every issue at once, which is why resolving collects them
// instead of returning the first. module names the module whose build the
// problem is about and is "" for the resolver's, the app name's and the
// manifest gates' problems; contract and requiredness are set only for a read
// a build could not answer.
type issue struct {
	method       string
	err          error
	module       string
	contract     string
	requiredness string
}

// resolve answers the declared app for one deployment. Every sentence it
// produces names the module, the contract and the fix; a follow-on error
// caused by a module that could not be resolved is suppressed, so one
// missing provider is one sentence.
// resolution is one run of the resolver: the plan it is building, the sentences it
// has produced, and which modules it has withdrawn or already refused. Every phase
// of resolve is a method of this one value, so the state one phase sets and
// the next
// reads is named where it lives rather than passed down a chain of calls.
type resolution struct {
	app    *App
	deploy Deployment
	p      *plan

	issues   []issue
	said     map[string]bool
	envKnown bool

	lives              []*Module
	chosen             map[*Module]bool
	survivingSuppliers map[reflect.Type][]*Module
	dead               map[*Module]bool
	refused            map[*Module]bool
	chosenOver         map[*Module]string
	mixed              map[reflect.Type]bool
}

// newResolution opens the run on the deployment's own configuration. The cloud
// profile is applied here, once, before anything reads a setting, so the sections
// modules read, the implementation a FromDeployment picks, the configuration the
// engine starts on and the seams Describe prints are one answer rather than four
// readings of one file. A Config built in Go never passed through Load, and
// resolving
// the profile only where the seams are answered would leave a deployment described
// from settings the composition never used. An unknown profile name fills nothing
// here and is refused with every other problem the deployment has.
func newResolution(a *App, d Deployment) *resolution {
	d.Config = config.ApplyCloudProfile(d.Config)
	return &resolution{
		app: a, deploy: d, said: map[string]bool{}, envKnown: true,
		chosen:             map[*Module]bool{},
		survivingSuppliers: map[reflect.Type][]*Module{},
		dead:               map[*Module]bool{},
		refused:            map[*Module]bool{},
		chosenOver:         map[*Module]string{},
		mixed:              map[reflect.Type]bool{},
		p: &plan{
			from:         map[edge]*Module{},
			deps:         map[*Module][]*Module{},
			contributors: map[reflect.Type][]*Module{},
			takers:       map[reflect.Type][]*Module{},
			impl:         map[*Module]string{},
			manifest:     map[*Module]module.Module{},
			cfg:          d.Config,
			reads:        map[*Module][]reflect.Type{},
		},
	}
}

// fail adds one sentence. Every one names the module or contract, and the fix.
func (r *resolution) fail(method, format string, args ...any) {
	r.issues = append(r.issues, issue{method: method, err: fmt.Errorf(format, args...)})
}

// failOnce says the sentence about one thing once, whoever asks about it next: a
// follow-on error caused by a module that could not be resolved would
// otherwise bury
// the one missing provider that explains all of them.
func (r *resolution) failOnce(id, method, format string, args ...any) {
	if r.said[id] {
		return
	}
	r.said[id] = true
	r.fail(method, format, args...)
}

// resolve answers the declared app for one deployment, phase by phase, and returns
// the plan together with every sentence the phases produced. A follow-on error
// caused
// by a module that could not be resolved stays unsaid, so one missing provider
// is one
// sentence rather than a paragraph per module that needed it.
func (a *App) resolve(d Deployment) (*plan, []issue) {
	r := newResolution(a, d)

	r.checkEnvironment()
	r.collectLivingModules()
	r.checkChoices()
	r.keepChosenSuppliers()
	r.checkSelfDeclarations()
	r.pickTheLastModule()
	r.checkMixedContracts()
	r.resolveNeeds()
	r.pickImplementations()
	r.resolveSeams()
	r.collectContributorsAndTakers()
	r.checkOrder()

	return r.p, r.issues
}

// checkEnvironment refuses a deployment whose environment is not one of the
// three, and records that nothing may be picked under it.
func (r *resolution) checkEnvironment() {
	// The environment is the deployment's, not each module's: it is checked
	// once, where the deployment is read, not on the way to picking one
	// implementation. Checked only there, a composition with no
	// FromDeployment module would resolve, build and Explain under a name that
	// is not an environment — and Development is the only spelling that lets a
	// simulated implementation through, so "prod" for "production" is one
	// dropped letter that changes which rules the app is resolved under while
	// the answer that should name the mistake stays unsaid.
	switch r.deploy.Environment {
	case Development, Staging, Production:
	default:
		r.fail("Deployment", "%q is not an environment: name development, staging or production", string(r.deploy.Environment))
		r.envKnown = false
	}
}

// collectLivingModules refuses a manifest named in Use twice and keeps the
// modules the composition actually holds, in the order Use gave them.
func (r *resolution) collectLivingModules() {
	// Duplicates: one manifest named twice is one manifest built twice.
	var lives []*Module
	byName := map[string]*Module{}
	for _, m := range r.app.uses {
		if _, dup := byName[m.name]; dup {
			r.fail("Use", "%s is in Use twice: remove one", m.name)
			continue
		}
		byName[m.name] = m
		lives = append(lives, m)
	}
	r.lives = lives
}

// checkChoices refuses a Choose that names a module Use never composed, and
// one that settles nothing.
func (r *resolution) checkChoices() {
	// Choose may only name a module Use composed, and it must settle a
	// contract that has more than one supplier; a choice that picks the
	// only provider of everything chooses nothing.
	for _, c := range r.app.chosen {
		if !contains(r.lives, c) {
			r.fail("Choose", "Choose(%s.Module) names a module that is not in Use: add it to Use or remove the choice", c.name)
			continue
		}
		r.chosen[c] = true
		contested := false
		for _, dl := range c.decls {
			if (dl.kind == provides || dl.kind == contributes) && len(suppliersOf(r.lives, dl.key)) > 1 {
				contested = true
			}
		}
		if !contested {
			r.fail("Choose", "Choose(%s.Module) chooses nothing: no contract it provides has a second provider", c.name)
		}
	}
}

// keepChosenSuppliers settles every contested contract: the chosen suppliers
// survive, the rest leave the composition, and the app records which was
// picked over which.
func (r *resolution) keepChosenSuppliers() {
	// Where a contract has several suppliers, the chosen ones survive and
	// the rest are not built at all: the app picked, so the loser's module
	// is not in the composition. A contract nobody takes is never ambiguous
	// to anyone, so nothing waits to be chosen for it.
	// dead says a module is not built; refused says a refusal already says why it
	// is not, so the modules that needed it may stay silent. The two are not the
	// same set: Choose withdraws the module it did not pick without refusing it,
	// because a choice is not a failure, and a withdrawn module says nothing of
	// its own — which is why missingNeed below asks who it was chosen over.
	for _, key := range declaredKeys(r.lives) {
		cand := suppliersOf(r.lives, key)
		picked := filter(cand, func(m *Module) bool { return r.chosen[m] })
		if len(picked) == 0 {
			r.survivingSuppliers[key] = cand
			continue
		}
		r.survivingSuppliers[key] = picked
		passed := names(filter(cand, func(m *Module) bool { return !r.chosen[m] }))
		for _, c := range picked {
			r.p.choices = append(r.p.choices, choice{key: key, picked: c, passed: passed})
		}
		if len(cand) > 1 {
			for _, m := range filter(cand, func(m *Module) bool { return !r.chosen[m] }) {
				r.dead[m] = true
				r.chosenOver[m] = andList(moduleNames(picked))
			}
		}
	}
}

// checkSelfDeclarations refuses a module that says one thing about itself
// twice, or that asks to be built before itself.
func (r *resolution) checkSelfDeclarations() {
	// What a module says about itself, it says once: the same declaration
	// twice would count it twice among the contributors to its contract, and
	// a module that needs the contract it supplies itself asks to be built
	// before itself, which no order can hold.
	for _, m := range r.lives {
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
					r.fail("Use", "%s takes every %s and contributes one itself: a module cannot be built before its own contribution",
						m.name, contract(dl.key))
					continue
				}
				if !dl.many && hasDeclKey(m, provides, dl.key) {
					r.fail("Use", "%s needs %s, which it provides itself: a module cannot be built before itself",
						m.name, contract(dl.key))
					continue
				}
				if !dl.many && hasDeclKey(m, contributes, dl.key) {
					r.fail("Use", "%s needs %s, which it contributes itself: a module cannot be built before its own contribution",
						m.name, contract(dl.key))
					continue
				}
				id.many = dl.many
			}
			if dl.kind == provides && hasDeclKey(m, contributes, dl.key) {
				r.fail("Use", "%s provides %s and contributes one %s: a contract is either the app's one provider or a contribution, not both in one module",
					m.name, contract(dl.key), contract(dl.key))
				continue
			}
			if once[id] {
				r.fail("Use", "%s declares it %s twice: a module says each thing once", m.name, contractOf(dl))
				continue
			}
			once[id] = true
		}
	}
}

// pickTheLastModule keeps the one module that may run after everything, and
// refuses the others that ask.
func (r *resolution) pickTheLastModule() {
	// One module may run after everything; the one that also asks is
	// refused before any of it means anything.
	var afters []*Module
	for _, m := range r.lives {
		if hasDecl(m, after) {
			afters = append(afters, m)
		}
	}
	if len(afters) > 1 {
		switch len(afters) {
		case 2:
			r.fail("Use", "%s and %s both ask to run after everything: only one module may", afters[0].name, afters[1].name)
		default:
			r.fail("Use", "%s ask to run after everything: only one module may", andList(names(afters)))
		}
		r.p.last = afters[0]
		for _, m := range afters[1:] {
			r.dead[m] = true
			r.refused[m] = true
		}
	} else if len(afters) == 1 {
		r.p.last = afters[0]
	}
}

// checkMixedContracts refuses a contract that one module provides and another
// contributes, which one app cannot answer twice.
func (r *resolution) checkMixedContracts() {
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
	for _, key := range declaredKeys(r.lives) {
		var providers, contributors []*Module
		for _, m := range suppliersOf(r.lives, key) {
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
		r.mixed[key] = true
		r.failOnce("mixed "+key.String(), "Use", "%s is provided by %s and contributed by %s: a contract is either the app's one provider or a contribution, not both in one app — make one of them the other; Choose cannot settle it",
			contract(key), andList(moduleNames(providers)), andList(moduleNames(contributors)))
	}
}

// missingNeed says why a hard need has no answer, and says it once: nothing
// supplies the contract at all, or the only supplier was chosen away.
func (r *resolution) missingNeed(m *Module, dl Declaration) {
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
	sup := r.survivingSuppliers[dl.key]
	id := "missing " + m.name + " " + dl.key.String()
	if len(sup) == 0 {
		r.failOnce(id, "Use", "%s needs %s: %s", m.name, contract(dl.key), r.app.fixFor(dl.key))
		return
	}
	for _, s := range sup {
		if r.refused[s] {
			continue
		}
		r.failOnce(id, "Use", "%s needs %s: %s.Module provides it and %s chose %s over it — Choose %s.Module or compose a module that provides %s",
			m.name, contract(dl.key), s.name, r.app.name, r.chosenOver[s], s.name, contract(dl.key))
		return
	}
}

// resolveNeeds wires every living module to a supplier of every need, and
// withdraws in silence whatever depends on a module a refusal already
// explained.
func (r *resolution) resolveNeeds() {
	// Every need is resolved for every living module, the module that runs
	// after everything included: it is built last, so whatever it needs is
	// already placed when it is asked for, and skipping it is how its own need
	// went unanswered — built with the zero value, no sentence.
	//
	// One supplier is an edge, none is the missing sentence (unless the supplier
	// exists but could not itself be resolved, the follow-on that stays unsaid),
	// several is the ambiguity sentence, said once for the contract. An unmet
	// need withdraws its supplier's takers too, and an optional need of such a
	// supplier goes unfilled, the way it does when no provider is composed.
	//
	// Refused in the same sweep is any dependency on the module that runs after
	// everything, whichever way it is spelled: whoever depended on it would have
	// to be built after it, which no order holds.
	for {
		changed := false
		for _, m := range r.lives {
			if r.dead[m] {
				continue
			}
			for _, dl := range m.decls {
				if dl.many || (dl.kind != needs && dl.kind != optional) {
					continue
				}
				e := edge{m, dl.key}
				if s := r.p.from[e]; s != nil {
					if !r.dead[s] {
						continue
					}
					if dl.kind == needs {
						// Its supplier died, and only under a refusal: Choose withdraws
						// modules before this sweep runs, so nothing withdrawn is ever
						// wired in here. This module is not built and says nothing; that
						// refusal is the composition's one sentence.
						r.dead[m] = true
						r.refused[m] = true
					} else {
						r.p.from[e] = nil
					}
					changed = true
					break
				}
				cand := survivingOf(r.survivingSuppliers[dl.key], r.dead)
				switch len(cand) {
				case 1:
					r.p.from[e] = cand[0]
				case 0:
					if dl.kind == optional {
						r.p.from[e] = nil
						continue
					}
					r.missingNeed(m, dl)
					r.dead[m] = true
					r.refused[m] = true
					changed = true
				default:
					switch {
					case r.mixed[dl.key]:
						// The pair is refused once for the contract above; a
						// second sentence here would name Choose, which is not
						// the fix for it.
					case allContributors(r.lives, cand) && dl.kind == needs:
						r.failOnce("ambiguous "+dl.key.String(), "Choose", "%s takes one %s, and %s: Choose in %s",
							firstTaker(r.lives, dl.key), dl.key.Name(), contributorsSay(r.lives, cand), r.app.name)
					default:
						r.failOnce("ambiguous "+dl.key.String(), "Choose", "%s has %s: Choose one in %s",
							contract(dl.key), andList(moduleNames(cand)), r.app.name)
					}
					r.dead[m] = true
					r.refused[m] = true
					changed = true
				}
			}
			if !r.dead[m] && r.p.last != nil && m != r.p.last {
				if r.app.dependsOnLast(r.p, r.fail, m) {
					r.dead[m] = true
					r.refused[m] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
}

// pickImplementations asks each living module what it picks from the
// deployment, under an environment the deployment named honestly.
func (r *resolution) pickImplementations() {
	// What the deployment picks, it picks honestly: a simulated
	// implementation runs in development only, and a real one only with
	// the inputs it reads. An environment it does not know is refused above,
	// once, and nothing is picked under it: whichever implementation it named
	// would be a guess at which rules the deployment was resolved under.
	if r.envKnown {
		for _, m := range r.lives {
			if r.dead[m] {
				continue
			}
			for _, dl := range m.decls {
				if dl.kind == fromDeployment {
					r.app.pickImplementation(r.p, r.fail, m, dl, r.deploy)
				}
			}
		}
	}
}

// resolveSeams records what the deployment's configuration answers about its
// seams, and what it still owes the cloud profile it named.
func (r *resolution) resolveSeams() {
	// What the deployment's configuration answers about its seams, and what it
	// still owes the cloud profile it named. Read here rather than at Build because
	// it is a problem with the deployment in the same sense a missing input is one,
	// and a person who is told about both at once fixes one file once.
	if r.envKnown {
		cloud, cloudIssues := resolveCloud(r.deploy)
		r.p.cloud = cloud
		r.issues = append(r.issues, cloudIssues...)
	}
}

// collectContributorsAndTakers records, for each contract, who contributes to
// it and who takes it, read off the survivors.
func (r *resolution) collectContributorsAndTakers() {
	// Contributors and takers, read off the survivors.
	for _, m := range r.lives {
		if r.dead[m] {
			continue
		}
		for _, dl := range m.decls {
			switch {
			case dl.kind == contributes:
				r.p.contributors[dl.key] = append(r.p.contributors[dl.key], m)
			case dl.kind == needs || dl.kind == optional:
				r.p.takers[dl.key] = append(r.p.takers[dl.key], m)
			}
		}
	}
}

// checkOrder places every surviving module in the build order, and says what
// no order answers: the cycle, and the module that waits for it.
func (r *resolution) checkOrder() {
	// The order, the cycle that has no order, and any module no order places.
	order, cycle, unplaced := r.app.order(r.lives, r.dead, r.p)
	r.p.order = order
	if cycle != nil {
		r.fail("Use", "%s is a cycle: %s", strings.Join(names(cycle), " → "), r.p.cycleReasons(cycle))
	}
	for _, m := range unplaced {
		r.fail("Use", "%s is in Use but no build order places it: it waits for %s, which no order reaches before it",
			m.name, andList(moduleNames(r.p.deps[m])))
	}
}
