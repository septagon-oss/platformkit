package pkit_test

// Every composition pkit answers "yes" to is a composition the client serves:
// Validate's nil is what Build will accept, and Explain is the file the client
// commits as COMPOSITION.<env>.md. So four things hold of every composition
// this package accepts, and no others:
//
//   - every module Use was given is in it, or the composition file names the
//     choice that withdrew it — Use's list is never quietly smaller;
//   - a hard need is never wired the zero value;
//   - a value never reaches a module from a module that was never built, and
//     the contributions a taker receives are exactly those of the modules that
//     were built — a module Choose withdrew takes its values with it, and the
//     taker must not read them;
//   - nothing panics where a sentence is owed.
//
// These are checked over every subset of eleven stand-in modules over three
// contracts — provider, provider-of-two, contributor-that-also-provides, hard
// taker, many-taker, the module that runs after everything, a FromDeployment
// module — combined with every choice of up to two of the contested providers,
// in development and in production. Choose and FromDeployment are in the sweep
// because they are where a module leaves the composition: an answer that is
// correct only while nobody chooses is not an answer.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

type cartValue struct{ from string }

func (c cartValue) Total() int64 { return 1 }

type payValue struct{ from string }

func (p payValue) Charge(minor int64) string { return p.from }

type builtRecord struct {
	seen   map[string]int
	cart   cartcontracts.Service
	pay    paymentcontracts.Provider
	exts   []cartcontracts.Extension
	marks  []string
	others map[string]int
}

func (r *builtRecord) reset() {
	r.seen, r.cart, r.pay, r.exts, r.marks, r.others = nil, nil, nil, nil, nil, nil
}

func sweepModules(rec *builtRecord) (all []*pkit.Module, contested []string) {
	newMod := func(name string, put func(*pkit.Wiring), decls ...pkit.Declaration) *pkit.Module {
		return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
			if rec.seen == nil {
				rec.seen = map[string]int{}
			}
			rec.seen[name]++
			if rec.others == nil {
				rec.others = map[string]int{}
			}
			rec.others[name] = len(pkit.Composition(w))
			if put != nil {
				put(w)
			}
			return module.Module{Name: name}, nil
		}, decls...)
	}
	return []*pkit.Module{
		newMod("alpha", func(w *pkit.Wiring) { pkit.Put[cartcontracts.Service](w, cartValue{"alpha"}) }, pkit.Provides[cartcontracts.Service]()),
		newMod("beta", func(w *pkit.Wiring) { pkit.Put[cartcontracts.Service](w, cartValue{"beta"}) }, pkit.Provides[cartcontracts.Service]()),
		newMod("gamma", func(w *pkit.Wiring) {
			pkit.Put[cartcontracts.Service](w, cartValue{"gamma"})
			pkit.Put[paymentcontracts.Provider](w, payValue{"gamma"})
		}, pkit.Provides[cartcontracts.Service](), pkit.Provides[paymentcontracts.Provider]()),
		newMod("delta", func(w *pkit.Wiring) { pkit.Put[paymentcontracts.Provider](w, payValue{"delta"}) }, pkit.Provides[paymentcontracts.Provider]()),
		newMod("contrib", func(w *pkit.Wiring) { pkit.Put[cartcontracts.Extension](w, cartcontracts.Extension{Module: "contrib"}) }, pkit.Contributes[cartcontracts.Extension]()),
		newMod("donor", func(w *pkit.Wiring) { pkit.Put[cartcontracts.Extension](w, cartcontracts.Extension{Module: "donor"}) },
			pkit.Provides[cartcontracts.Service](), pkit.Contributes[cartcontracts.Extension]()),
		newMod("taker", func(w *pkit.Wiring) { rec.cart = pkit.Get[cartcontracts.Service](w) }, pkit.Needs[cartcontracts.Service]()),
		newMod("payer", func(w *pkit.Wiring) { rec.pay = pkit.Get[paymentcontracts.Provider](w) }, pkit.Needs[paymentcontracts.Provider]()),
		newMod("collector", func(w *pkit.Wiring) { rec.exts = pkit.All[cartcontracts.Extension](w) }, pkit.Needs[[]cartcontracts.Extension]()),
		newMod("marks", func(w *pkit.Wiring) { rec.marks = append(rec.marks, fmt.Sprint(len(pkit.Composition(w)))) }, pkit.After(pkit.Everything)),
		newMod("deployed", nil, pkit.FromDeployment(
			pkit.Implementation{Name: "real", Inputs: []string{"api_key"}},
			pkit.Implementation{Name: "sim", Simulated: true})),
	}, []string{"alpha", "beta", "gamma", "delta", "donor"}
}

func TestEveryCompositionThatValidateAcceptsIsTheCompositionExplainDescribes(t *testing.T) {
	byName := map[string]*pkit.Module{}
	rec := &builtRecord{}
	all, contested := sweepModules(rec)
	for _, m := range all {
		byName[m.Name()] = m
	}
	deployments := []pkit.Deployment{
		{Environment: pkit.Development, Inputs: map[string]string{"api_key": "set"}},
		{Environment: pkit.Production, Inputs: map[string]string{"api_key": "set"}},
		{Environment: pkit.Development},
	}
	checked, accepted, withdrew, withdrewAContribution := 0, 0, 0, 0
	for use := 1; use < 1<<len(all); use++ {
		var useNames []string
		for i, m := range all {
			if use>>i&1 == 1 {
				useNames = append(useNames, m.Name())
			}
		}
		for _, choice := range choiceSets(contested) {
			for _, d := range deployments {
				rec.reset()
				app := pkit.NewApp("collect")
				for _, n := range useNames {
					app = app.Use(byName[n])
				}
				for _, n := range choice {
					app = app.Choose(byName[n])
				}
				err := report(t, useNames, choice, d, func() error { return app.Validate(d) })
				checked++
				if err != nil {
					continue
				}
				accepted++
				// Explain composes and builds again: what is checked below is what
				// this run built, so the record of Validate's run is cleared first.
				rec.reset()
				text, xerr := app.Explain(d)
				if xerr != nil {
					t.Fatalf("%s: Explain refused a composition Validate accepted: %v", spell(useNames, choice, d), xerr)
				}
				if strings.Contains(text, "chose") {
					withdrew++
					for _, n := range []string{"contrib", "donor"} {
						if rec.seen[n] == 0 && rec.seen["collector"] == 1 && contains(useNames, n) {
							withdrewAContribution++
						}
					}
				}
				for _, problem := range silentProblems(rec, useNames, text) {
					t.Errorf("%s: %s\n%s", spell(useNames, choice, d), problem, text)
				}
			}
		}
	}
	t.Logf("sweep: %d compositions checked, %d accepted, %d of those with a module the deployment chose out of them, %d of those with the taker of every contribution built beside a contribution that was chosen out", checked, accepted, withdrew, withdrewAContribution)
}

// silentProblems are the ways an accepted composition can disagree with itself.
func silentProblems(rec *builtRecord, useNames []string, text string) []string {
	var problems []string
	for _, n := range useNames {
		built, chosenOut := rec.seen[n], chosenOut(text)[n]
		switch {
		case built > 1:
			problems = append(problems, fmt.Sprintf("%s.Module was built %d times, and Use named it once", n, built))
		case built == 0 && !chosenOut:
			problems = append(problems, fmt.Sprintf("%s is in Use but was never built, and no choice names it as the module the app chose out", n))
		case built == 1 && chosenOut:
			problems = append(problems, fmt.Sprintf("%s is named as chosen out of the composition and was built anyway", n))
		}
	}
	if head := strings.SplitN(text, "\n", 2)[0]; !strings.Contains(head, fmt.Sprintf("builds %d ", len(rec.seen))) {
		problems = append(problems, fmt.Sprintf("the composition file counts modules it did not build: %q over %d built", head, len(rec.seen)))
	}
	if rec.seen["taker"] == 1 {
		if rec.cart == nil {
			problems = append(problems, "taker needs cartcontracts.Service and was wired the zero value")
		} else if v, ok := rec.cart.(cartValue); !ok || rec.seen[v.from] != 1 {
			problems = append(problems, "taker reads cartcontracts.Service from a module that was never built")
		}
	}
	if rec.seen["payer"] == 1 {
		if rec.pay == nil {
			problems = append(problems, "payer needs paymentcontracts.Provider and was wired the zero value")
		} else if v, ok := rec.pay.(payValue); !ok || rec.seen[v.from] != 1 {
			problems = append(problems, "payer reads paymentcontracts.Provider from a module that was never built")
		}
	}
	for name, seen := range rec.others {
		if name == "marks" {
			continue
		}
		if seen != 0 && rec.seen[name] == 1 {
			problems = append(problems, fmt.Sprintf("%s.Module read %d other modules' manifests, and only the module that runs after everything is given them", name, seen))
		}
	}
	if len(rec.marks) != 0 {
		for _, mark := range rec.marks {
			if mark != fmt.Sprint(len(rec.seen)-1) {
				problems = append(problems, fmt.Sprintf("the module that runs after everything saw %s manifests of %d built modules", mark, len(rec.seen)))
			}
		}
	}
	if rec.seen["collector"] == 1 {
		var want []string
		for _, n := range []string{"contrib", "donor"} {
			if rec.seen[n] == 1 {
				want = append(want, n)
			}
		}
		if len(rec.exts) != len(want) {
			problems = append(problems, fmt.Sprintf("collector takes %d extensions, and %d composed modules contribute one: %v", len(rec.exts), len(want), rec.exts))
		}
		for _, e := range rec.exts {
			if rec.seen[e.Module] != 1 {
				problems = append(problems, fmt.Sprintf("collector takes an extension from %s, which was never built", e.Module))
			}
		}
	}
	return problems
}

// choiceSets is every set of up to two contested providers.
func choiceSets(contested []string) [][]string {
	out := [][]string{nil}
	for i, a := range contested {
		out = append(out, []string{a})
		for j := i + 1; j < len(contested); j++ {
			out = append(out, []string{a, contested[j]})
		}
	}
	return out
}

// report runs one composition and names it in whatever it answers, so a panic
// arrives as a finding about that composition rather than a stack trace.
func report(t *testing.T, use, choice []string, d pkit.Deployment, run func() error) (err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("PANIC")
			t.Errorf("%s answered a composition by panicking: %v", spell(use, choice, d), p)
		}
	}()
	return run()
}

// chosenOut reads the modules the composition file says the app chose out of it.
func chosenOut(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "chose ") || !strings.Contains(line, " over ") {
			continue
		}
		list := strings.SplitN(line, " over ", 2)[1]
		if cut := strings.Index(list, " for "); cut >= 0 {
			list = list[:cut]
		}
		list = strings.NewReplacer(", ", " ", " and ", " ").Replace(list)
		for _, name := range strings.Fields(list) {
			out[strings.TrimSuffix(name, ".Module")] = true
		}
	}
	return out
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func spell(use, choice []string, d pkit.Deployment) string {
	s := "Use(" + strings.Join(use, " ") + ")"
	if len(choice) > 0 {
		s += " Choose(" + strings.Join(choice, " ") + ")"
	}
	return s + " in " + string(d.Environment)
}
