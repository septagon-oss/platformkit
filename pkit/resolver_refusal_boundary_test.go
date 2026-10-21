package pkit_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"

	engine "github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

func TestResolverReadRefusalStopsEveryCompositionEntryPoint(t *testing.T) {
	cfg := onOneDatabase(t)
	deployment := pkit.Deployment{Environment: pkit.Development, Config: cfg}
	for _, collection := range []bool{false, true} {
		kind := "scalar"
		if collection {
			kind = "collection"
		}
		for _, entry := range []string{"Validate", "Plan", "Build", "Run", "Explain", "Describe"} {
			t.Run(kind+"/"+entry, func(t *testing.T) {
				continued, unwound, dependentRan := false, false, false
				var manifests []string
				provides, needs := pkit.Provides[heldPort](), pkit.Needs[heldPort]()
				if collection {
					provides, needs = pkit.Contributes[heldPort](), pkit.Needs[[]heldPort]()
				}
				broken := pkit.NewModule("broken", func(w *pkit.Wiring) (module.Module, error) {
					defer func() { unwound = true }()
					pkit.Put[heldPort](w, holder{})
					if collection {
						_ = pkit.All[readPort](w)
					} else {
						_ = pkit.Get[readPort](w)
					}
					continued = true
					return module.Module{Name: "broken"}, nil
				}, provides)
				dependent := pkit.NewModule("dependent", func(*pkit.Wiring) (module.Module, error) {
					dependentRan = true
					return module.Module{Name: "dependent"}, nil
				}, needs)
				other := pkit.NewModule("other", func(*pkit.Wiring) (module.Module, error) {
					return module.Module{Name: "other"}, errOther
				})
				last := pkit.NewModule("last", func(w *pkit.Wiring) (module.Module, error) {
					for _, manifest := range pkit.Composition(w) {
						manifests = append(manifests, manifest.Name)
					}
					return module.Module{Name: "last"}, nil
				}, pkit.After(pkit.Everything))
				a := pkit.NewApp("collect").Use(user.Module, broken, dependent, other, last)
				var err error
				var doc *pkit.Description
				switch entry {
				case "Validate":
					err = a.Validate(deployment)
				case "Plan":
					var plan *pkit.Planned
					plan, err = a.Plan(deployment)
					if plan != nil {
						t.Error("refused Plan returned partial outputs")
					}
				case "Build":
					var runtime *pkit.Runtime
					runtime, err = a.Build(t.Context(), deployment)
					if runtime != nil {
						defer runtime.Close()
						t.Error("refused Build returned a runtime")
					}
				case "Run":
					err = a.Run(t.Context(), deployment, engine.All)
				case "Explain":
					var explanation string
					explanation, err = a.Explain(deployment)
					if explanation != "" {
						t.Errorf("refused Explain returned a graph: %s", explanation)
					}
				case "Describe":
					doc, err = a.Describe(deployment)
				}
				ce, ok := errors.AsType[*pkit.CompositionError](err)
				if !ok {
					t.Fatalf("refusal is not a CompositionError: %v", err)
				}
				if ce.App != "collect" || len(ce.Problems) != 2 {
					t.Fatalf("want two independent problems in collect: %+v", ce)
				}
				first := ce.Problems[0]
				if first.Cause != "Build" || first.Module != "broken" || first.Contract != "pkit_test.readPort" || first.Requiredness != "undeclared" {
					t.Errorf("read refusal lost its attribution: %+v", first)
				}
				if ce.Problems[1].Module != "other" || !errors.Is(err, errOther) {
					t.Errorf("independent failure was lost: %+v", ce.Problems)
				}
				if !slices.Equal(ce.Suppressed, []pkit.DescribedSuppression{{Module: "dependent", By: "broken"}}) {
					t.Errorf("dependent was not suppressed by broken: %+v", ce.Suppressed)
				}
				if continued || !unwound || dependentRan {
					t.Errorf("continued=%v, unwound=%v, dependentRan=%v; want false, true, false", continued, unwound, dependentRan)
				}
				if !slices.Equal(manifests, []string{"user"}) {
					t.Errorf("after-everything saw partial outputs: %v", manifests)
				}
				if entry == "Describe" {
					if doc == nil || doc.Resolved || len(doc.Modules) != 0 {
						t.Fatalf("refused Describe returned a graph: %+v", doc)
					}
					if !reflect.DeepEqual(doc.Problems, ce.Problems) || !slices.Equal(doc.Suppressed, ce.Suppressed) {
						t.Errorf("document and error disagree: %+v; %+v", doc, ce)
					}
				}
			})
		}
	}
	if n := tablesIn(t, cfg.Database.MigrateURL); n != 0 {
		t.Errorf("refused composition left %d tables, including any migration or outbox ledger", n)
	}
}

func TestConcurrentDescriptionsKeepResolverRefusalsIndependent(t *testing.T) {
	broken := pkit.NewModule("broken", func(w *pkit.Wiring) (module.Module, error) {
		_ = pkit.All[readPort](w)
		return module.Module{Name: "broken"}, nil
	})
	a := pkit.NewApp("collect").Use(user.Module, broken)
	doc, err := a.Describe(dev)
	if _, ok := errors.AsType[*pkit.CompositionError](err); !ok {
		t.Fatalf("first description did not refuse: %v", err)
	}
	want, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			for range 8 {
				got, err := a.Describe(dev)
				if _, ok := errors.AsType[*pkit.CompositionError](err); !ok {
					t.Errorf("concurrent description lost its refusal: %v", err)
					return
				}
				data, err := json.Marshal(got)
				if err != nil || string(data) != string(want) {
					t.Errorf("concurrent description changed: %s; error %v; want %s", data, err, want)
				}
			}
		})
	}
	wg.Wait()
}

func TestNestedCompositionDoesNotRecoverItsCallersWiringFailure(t *testing.T) {
	continued := false
	outer := pkit.NewModule("outer", func(w *pkit.Wiring) (module.Module, error) {
		inner := pkit.NewModule("inner", func(*pkit.Wiring) (module.Module, error) {
			_ = pkit.Get[readPort](w)
			return module.Module{Name: "inner"}, nil
		})
		_ = pkit.NewApp("nested").Use(inner).Validate(dev)
		continued = true
		return module.Module{Name: "outer"}, nil
	})
	err := pkit.NewApp("collect").Use(outer).Validate(dev)
	ce, ok := errors.AsType[*pkit.CompositionError](err)
	if !ok || len(ce.Problems) != 1 {
		t.Fatalf("outer read did not refuse its own composition: %v", err)
	}
	if continued || ce.App != "collect" || ce.Problems[0].Module != "outer" {
		t.Errorf("another composition consumed the failed read: continued=%v, error=%+v", continued, ce)
	}
}
