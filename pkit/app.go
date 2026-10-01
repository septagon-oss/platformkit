package pkit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/module"
)

// App is a name and the modules it uses. Its methods record; nothing is
// checked until the composition is resolved.
type App struct {
	name   string
	uses   []*Module
	chosen []*Module
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
// of the three environments above, and the inputs an implementation picked by
// FromDeployment reads. Any other name is refused before anything is picked.
type Deployment struct {
	Environment Environment
	Inputs      map[string]string
}

// Wiring is what a module's build reads and writes: the values it declared it
// needs, and the ones it declared it provides or contributes.
type Wiring struct {
	at     *Module
	plan   *plan
	values map[*Module]map[any][]any
	built  []module.Module
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
	for _, s := range man.Subscriptions {
		subs = append(subs, s.Name)
	}
	if len(subs) > 0 {
		fmt.Fprintf(&b, "pkit: %s.Module handles %s.\n", man.Name, andList(subs))
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
func (a *App) compose(d Deployment) (*plan, []module.Module, error) {
	p, issues := a.resolve(d)
	var errs []error
	for _, in := range issues {
		errs = append(errs, fmt.Errorf("pkit: %s: %s: %w", a.name, in.method, in.err))
	}
	if len(errs) > 0 {
		return p, nil, errors.Join(errs...)
	}
	w := &Wiring{plan: p, values: map[*Module]map[any][]any{}}
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
		return p, nil, errors.Join(errs...)
	}
	return p, w.built, nil
}

// Validate answers every problem with the composition — duplicates, missing,
// ambiguous or doubly-supplied providers, cycles, phase violations, an
// environment the deployment did not name and the inputs an implementation is
// missing — at once, each naming the method that caused it. The modules' build
// functions run before the answer is given, because the value a module Puts is
// only observable from inside its own build; a refused composition returns
// nothing it built. It is the phase that runs before the app is served, not a
// phase in which none of your code runs.
func (a *App) Validate(d Deployment) error {
	_, _, err := a.compose(d)
	return err
}

// Explain reads the resolved composition, not the declared one, for one
// named environment: the build order, who provides what to whom, the
// contributions, the choices, the implementations the deployment picked, the
// permissions and events each built module describes to the kernel, and what
// each module describes about itself (decision 0074 rule 4). Routes and tenant
// hosts are the two parts of rule 4 this cannot say yet: a manifest registers
// its routes through a function rather than listing them, and the server that
// would mount them is not composed here.
func (a *App) Explain(d Deployment) (string, error) {
	p, built, err := a.compose(d)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	noun := "modules"
	if len(built) == 1 {
		noun = "module"
	}
	fmt.Fprintf(&b, "pkit: %s in %s builds %d %s.\n", a.name, d.Environment, len(built), noun)
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
		for _, dl := range m.decls {
			switch {
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
	return b.String(), nil
}
