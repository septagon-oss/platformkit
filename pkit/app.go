package pkit

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// App is a name and the modules it uses. Its methods record; nothing is
// checked until the composition is resolved.
type App struct {
	name   string
	uses   []*Module
	chosen []*Module

	// What the application decides about how it looks and reads, and the two
	// doors it owns. Recorded by skin.go and read by Plan; every field here is
	// read by a phase, which is the test a recorded value has to pass to exist.
	// The three counters say how many times a customisation was named, which is
	// what Build refuses: a chain records, so a second Theme is not an error at
	// the moment somebody types it, and it must not silently win either.
	theme    design.Pair
	themes   int
	homes    []string
	copy     Catalogue
	copies   int
	roles    []Role
	refusal  func(Skin) httpx.Fault
	refusals int
	ask      httpx.AskForAccess
	askPage  func(router *httpx.Router)
	catalog  func(api *httpx.API)

	// built says this App has handed its lifecycle to the engine. kit/app
	// documents one Start per App; a chain that records cannot honestly be
	// re-composed after one, so a second Build is refused rather than attempted.
	// buildMu is what makes that refusal hold when the two Builds arrive at
	// the same moment: one App is one lifecycle, and two calls that overlap must
	// not both reach the engine. A boot the engine refused before it reached the
	// deployment never had a lifecycle, and release says so — see build.go.
	buildMu sync.Mutex
	built   bool
}

// NewApp starts an app.
func NewApp(name string) *App { return &App{name: name} }

// Use composes these modules into the app.
func (a *App) Use(ms ...*Module) *App { a.uses = append(a.uses, ms...); return a }

// Choose picks these modules where two composed modules offer one contract.
func (a *App) Choose(ms ...*Module) *App { a.chosen = append(a.chosen, ms...); return a }

// Environment names where a deployment runs; outside Development a simulated
// implementation is refused.
type Environment string

const (
	Development Environment = "development"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

// Deployment is what the process supplies to a composition: where it runs, one
// of the three environments above, the inputs an implementation picked by
// FromDeployment reads, and the configuration the engine runs under. Any other
// name is refused before anything is picked.
//
// Config belongs here rather than on App because there is no environment-free
// truth about a composition (0074 rule 4): the same app is Stripe in one
// environment and simulated in another, and Validate, Explain and Build all
// answer about one named deployment. The transport fields and Caches are here for
// the same reason and are the process's rather than the app's: kit/app knows the
// names memory, jetstream and valkey and the rule between them and builds none of
// them, so the process that links the provider packages names the constructors
// here and nowhere else.
type Deployment struct {
	Environment Environment
	Inputs      map[string]string
	Config      config.Config

	// Transports are the two constructors nats.transport's two names map to.
	Transports app.Transports
	// Transport is the explicit override: a process that already holds a
	// transport hands it over instead of naming a constructor.
	Transport events.Transport

	// Caches are the constructors cache.adapter's one name maps to, for the same
	// reason Transports is here: kit/app knows the name valkey and the rule between
	// the adapters and links no client, so the process that imports the provider
	// package names how to reach one here. A deployment that sets cache.adapter to
	// valkey and leaves this empty is refused by kit/app before anything is opened.
	Caches app.Caches
}

// Wiring is what a module's build reads and writes: the values it declared it
// needs, and the ones it declared it provides or contributes.
type Wiring struct {
	at     *Module
	plan   *plan
	values map[*Module]map[any][]any
	built  []module.Module
	skin   Skin
	errs   []error
}

// Get is the one T a module declared with Needs or Optional; the zero value
// when an optional T is not composed.
func Get[T any](w *Wiring) T {
	var zero T
	d, ok := w.declared(Needs[T](), Optional[T]())
	if !ok || d.many {
		w.errs = append(w.errs, fmt.Errorf("%s reads %s, which it did not declare with Needs or Optional", w.at.name, contract(d.key)))
		return zero
	}
	from := w.plan.from[edge{w.at, d.key}]
	if from == nil {
		return zero
	}
	put := w.values[from][d.key]
	if len(put) != 1 {
		w.errs = append(w.errs, fmt.Errorf("%s needs %s, which %s.Module put %d of: it declares that it is the one %s it is",
			w.at.name, contract(d.key), from.name, len(put), contract(d.key)))
		return zero
	}
	v, _ := put[0].(T)
	return v
}

// All is every E contributed to a module that declared Needs[[]E] or Optional[[]E].
func All[E any](w *Wiring) []E {
	d, ok := w.declared(Needs[[]E](), Optional[[]E]())
	if !ok {
		w.errs = append(w.errs, fmt.Errorf("%s reads every %s, which it did not declare with Needs[[]%[2]s]", w.at.name, contract(d.key)))
		return nil
	}
	var all []E
	for _, m := range w.plan.contributors[d.key] {
		for _, v := range w.values[m][d.key] {
			e, ok := v.(E)
			if !ok {
				w.errs = append(w.errs, fmt.Errorf("%s.Module declares it contributes one %s and put no %s it is",
					m.name, contract(d.key), contract(d.key)))
				continue
			}
			all = append(all, e)
		}
	}
	return all
}

// Put is the T a module declared it provides or contributes.
func Put[T any](w *Wiring, v T) {
	key := Provides[T]().key
	if !w.at.declares(key, provides, contributes) {
		w.errs = append(w.errs, fmt.Errorf("%s puts %s, which it did not declare with Provides or Contributes", w.at.name, contract(key)))
		return
	}
	if w.values[w.at] == nil {
		w.values[w.at] = map[any][]any{}
	}
	w.values[w.at][key] = append(w.values[w.at][key], v)
}

// Composition is every other module's manifest, for the module that runs
// after everything; nil for any other.
func Composition(w *Wiring) []module.Module {
	if w.at != w.plan.last {
		return nil
	}
	return w.built
}

// Implementation is the name of the implementation the deployment picked for
// this module, or "" when it declares no FromDeployment.
func (w *Wiring) Implementation() string { return w.plan.impl[w.at] }

// Config reads one section of the deployment's configuration, for the module
// that cannot decide one of its own settings. The module names the section by a
// function it writes, so which section it reads is visible at the call site, in
// this module's source and in Explain, and the whole configuration is never
// handed round: a module that took config.Config would be coupled to every
// setting the kernel has, and nothing could say who read what.
//
// A deployment is where a setting lives (0074 rule 4), and a module build has no
// other route to one: FromDeployment picks the *name* of an implementation from
// the inputs it requires, it never hands over a value. The zero T is what a
// section the deployment says nothing about answers, which is why a module keeps
// its own default rather than inheriting a test's. Naming no section at all is a
// defect in that module, and Build is where a module's own defects are answered.
func Config[T any](w *Wiring, section func(config.Config) T) T {
	var zero T
	if section == nil {
		w.errs = append(w.errs, fmt.Errorf("%s reads a configuration section without naming which", w.at.name))
		return zero
	}
	t := reflect.TypeFor[T]()
	w.plan.reads[w.at] = append(w.plan.reads[w.at], t)
	return section(w.plan.cfg)
}

// Skin is what the application recorded about how it looks and reads: the pair
// of palettes it named with Theme, the front door it named with Home, the copy
// it named with Languages. A module that draws with them takes them from here
// rather than from a value the composition holds, so there stays one statement
// of a client's colours.
//
// Skin.Label answers "" during a build: the words a permission is given belong
// to the module that defines it, and no other module's manifest is a built thing
// while one is being built. Only the module that runs after everything sees them
// — it has Composition for that — and Planned.Skin carries them for the app's
// own renderers.
func (w *Wiring) Skin() Skin { return w.skin }

func (w *Wiring) declared(forms ...Declaration) (Declaration, bool) {
	for _, d := range w.at.decls {
		for _, f := range forms {
			if d.kind == f.kind && d.key == f.key && d.many == f.many {
				return d, true
			}
		}
	}
	return forms[0], false
}

// describesToKernel is what one built module told the kernel about itself:
// the permissions it defines and the events it emits and handles. Decision
// 0074 rule 4 puts these in the composition file a client commits, and the
// manifest is the one place they are written down — read through Module.Emits,
// the kernel's own union of the two spellings, so the composition file and the
// AsyncAPI document cannot disagree about what the app emits.
func describesToKernel(man module.Module) string {
	var b strings.Builder
	var perms []string
	for _, p := range man.Permissions {
		perms = append(perms, p.Key)
	}
	if len(perms) > 0 {
		fmt.Fprintf(&b, "pkit: %s.Module defines the permission %s.\n", man.Name, andList(perms))
	}
	var emits []string
	for _, e := range man.Emits() {
		emits = append(emits, e.Name)
	}
	if len(emits) > 0 {
		fmt.Fprintf(&b, "pkit: %s.Module emits %s.\n", man.Name, andList(emits))
	}
	var subs []string
	takes := "every event this application emits"
	if man.SubscribeAll {
		// The manifest of a SubscribeAll module carries one subscription with no
		// name — the wildcard, which module.Expand turns into one per event and
		// which the built manifest still shows as it was written. Naming nothing
		// here would print "handles ."
		for _, s := range man.Subscriptions {
			if s.Name != "" {
				subs = append(subs, s.Name)
			}
		}
	} else {
		takes = ""
		for _, s := range man.Subscriptions {
			subs = append(subs, s.Name)
		}
	}
	if takes == "" && len(subs) > 0 {
		takes = andList(subs)
	}
	if takes != "" {
		fmt.Fprintf(&b, "pkit: %s.Module handles %s.\n", man.Name, takes)
	}
	return b.String()
}

func (m *Module) declares(key any, kinds ...kind) bool {
	for _, d := range m.decls {
		for _, k := range kinds {
			if d.kind == k && d.key == key {
				return true
			}
		}
	}
	return false
}

// compose resolves the app for one deployment and builds every surviving
// module in order. It is Build's validate phase plus the modules' own dry
// construction: every problem is answered at once, each naming the method
// that caused it, and nothing it does opens a connection. No module is built
// while the composition still owes a sentence — the build of one module reads
// what another put, so building a composition that does not resolve is how a
// mistake turns into a crash instead of an answer.
func (a *App) compose(d Deployment) (*plan, []module.Module, map[*Module]map[any][]any, error) {
	p, issues := a.resolve(d)
	var errs []error
	for _, in := range issues {
		errs = append(errs, fmt.Errorf("pkit: %s: %s: %w", a.name, in.method, in.err))
	}
	if len(errs) > 0 {
		return p, nil, nil, errors.Join(errs...)
	}
	w := &Wiring{plan: p, values: map[*Module]map[any][]any{}, skin: a.recordedSkin()}
	for _, m := range p.order {
		w.at, w.errs = m, nil
		manifest, err := m.build(w)
		if err != nil {
			w.errs = append(w.errs, fmt.Errorf("%s: %w", m.name, err))
		}
		for _, dl := range m.decls {
			switch put := len(w.values[m][dl.key]); {
			case dl.kind == provides && put != 1:
				w.errs = append(w.errs, fmt.Errorf("%s declares it provides %s and did not put it once", m.name, contract(dl.key)))
			case dl.kind == contributes && put == 0:
				w.errs = append(w.errs, fmt.Errorf("%s declares it contributes one %s and did not put one: put one in Build, or do not declare it", m.name, contract(dl.key)))
			case dl.kind == contributes && put > 1:
				w.errs = append(w.errs, fmt.Errorf("%s declares it contributes one %s and put %d: a module contributes the one %s it is", m.name, contract(dl.key), put, contract(dl.key)))
			}
		}
		for _, e := range w.errs {
			errs = append(errs, fmt.Errorf("pkit: %s: Build: %w", a.name, e))
		}
		p.manifest[m] = manifest
		w.built = append(w.built, manifest)
	}
	if len(errs) > 0 {
		return p, nil, nil, errors.Join(errs...)
	}
	// The kernel's own manifest gates, run over what the dry build produced
	// rather than over a list somebody re-wrote: a module that subscribes to
	// everything is given the names here, and a subscription to a name nobody
	// emits is refused here — before a migration rather than at the first event.
	// This is 0074 rule 1's "every problem before the first effect" for the two
	// gates kit/app would otherwise answer after opening a connection.
	expanded := module.Expand(append([]module.Module{}, w.built...))
	if err := module.Validate(expanded); err != nil {
		return p, nil, nil, errors.Join(append(errs, fmt.Errorf("pkit: %s: Build: %w", a.name, err))...)
	}
	return p, w.built, w.values, nil
}

// Validate answers every problem with the composition — duplicates, missing,
// ambiguous or doubly-supplied providers, cycles, phase violations, an
// environment the deployment did not name, the inputs an implementation is
// missing, a contribution nobody takes and a module's own build refusing — at
// once, each naming the method that caused it. The modules' build functions run
// before the answer is given, because the value a module Puts is only
// observable from inside its own build; a refused composition returns nothing it
// built. It is the phase that runs before the app is served, not a phase in
// which none of your code runs. Build answers everything Validate answers, and
// the ports and the recorded roles besides.
func (a *App) Validate(d Deployment) error {
	_, _, _, err := a.compose(d)
	return err
}

// Explain reads the resolved composition, not the declared one, for one
// named environment: the build order, who provides what to whom, the
// contributions, the choices, the implementations the deployment picked, the
// permissions and events each built module describes to the kernel, what each
// module describes about itself (decision 0074 rule 4), and the roles the app
// recorded. Routes and tenant hosts are the two parts of rule 4 this cannot say
// yet: a manifest registers its routes through a function rather than listing
// them, and the server that would mount them is not composed here.
func (a *App) Explain(d Deployment) (string, error) {
	p, built, puts, err := a.compose(d)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	noun := "modules"
	if len(built) == 1 {
		noun = "module"
	}
	fmt.Fprintf(&b, "pkit: %s in %s builds %d %s.\n", a.name, d.Environment, len(built), noun)
	for _, r := range a.roles {
		held := "nothing"
		if len(r.Grants) > 0 {
			held = andList(r.Grants)
		}
		fmt.Fprintf(&b, "pkit: a tenant of %s begins as %s, holding %s.\n", a.name, r.Name, held)
	}
	for _, m := range p.order {
		if m != p.last {
			switch deps := p.deps[m]; len(deps) {
			case 0:
				fmt.Fprintf(&b, "pkit: %s.Module needs no other module.\n", m.name)
			default:
				fmt.Fprintf(&b, "pkit: %s.Module is built after %s.\n", m.name, andList(moduleNames(deps)))
			}
		}
		for _, dl := range m.decls {
			switch {
			case dl.kind == needs && !dl.many:
				fmt.Fprintf(&b, "pkit: %s.Module needs %s from %s.Module.\n", m.name, contract(dl.key), p.from[edge{m, dl.key}].name)
			case dl.kind == optional && !dl.many:
				if s := p.from[edge{m, dl.key}]; s != nil {
					fmt.Fprintf(&b, "pkit: %s.Module uses %s from %s.Module.\n", m.name, contract(dl.key), s.name)
				} else {
					fmt.Fprintf(&b, "pkit: %s.Module could use %s; %s composes no provider, so it will not.\n", m.name, contract(dl.key), a.name)
				}
			case (dl.kind == needs || dl.kind == optional) && dl.many:
				fmt.Fprintf(&b, "pkit: %s.Module takes every %s from %s.\n", m.name, contract(dl.key), whoOrNobody(moduleNames(p.contributors[dl.key])))
			}
		}
		// A module that reads a setting says so in the composition file, the same
		// way it says which module it needs: nothing a build reads stays hidden
		// (0074 rule 4). One line per section, in the order they were asked for.
		for _, t := range p.reads[m] {
			fmt.Fprintf(&b, "pkit: %s.Module reads %s.\n", m.name, contract(t))
		}
		for _, dl := range m.decls {
			switch {
			case dl.kind == contributes && len(p.takers[dl.key]) == 0:
				// Decided here (0074 rule 4, and T-0225's open question): a
				// contribution no composed module takes is a note, not a refusal.
				// A taker declares Needs[[]E], which the resolver reads as zero or
				// more, and an app that composes the contributor without the taker
				// has made a composition with an unread contribution — a fact a
				// reader has to be told, and not a mistake that stops the app: the
				// fixture's wishlist and collectibles both contribute an extension
				// to a cart that need not be composed at all.
				fmt.Fprintf(&b, "pkit: %s.Module contributes one %s; no module in %s takes one.\n", m.name, contract(dl.key), a.name)
			case dl.kind == contributes:
				fmt.Fprintf(&b, "pkit: %s.Module contributes one %s to %s.\n", m.name, contract(dl.key), whoOrNobody(moduleNames(p.takers[dl.key])))
			case dl.kind == fromDeployment:
				fmt.Fprintf(&b, "pkit: %s.Module runs on %s, which the deployment picked.\n", m.name, p.impl[m])
			case dl.kind == describes:
				fmt.Fprintf(&b, "pkit: %s.Module describes %v.\n", m.name, dl.about)
			case dl.kind == after:
				fmt.Fprintf(&b, "pkit: %s.Module runs after everything and saw every other module's manifest.\n", m.name)
			}
		}
		b.WriteString(describesToKernel(p.manifest[m]))
	}
	for _, c := range p.choices {
		if len(c.passed) > 0 {
			fmt.Fprintf(&b, "pkit: %s chose %s.Module over %s for %s.\n", a.name, c.picked.name, andList(c.passed), contract(c.key))
		}
	}
	// A contract nobody needs is a note, never a refusal. It is sometimes
	// exactly right: the ports the kernel asks the application are read by pkit
	// rather than by a module, and a module that reads a built manifest takes no
	// contract to do it. Silence is the state 0074 rule 4 refuses, so the line
	// says who provides it and who does not need it, and the reader decides.
	for _, m := range p.order {
		for _, dl := range m.decls {
			if dl.kind == provides && len(p.takers[dl.key]) == 0 && len(puts[m][dl.key]) > 0 {
				fmt.Fprintf(&b, "pkit: %s.Module provides %s; no module in %s needs it.\n", m.name, contract(dl.key), a.name)
			}
		}
	}
	return b.String(), nil
}
