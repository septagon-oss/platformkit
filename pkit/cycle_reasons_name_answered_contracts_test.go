package pkit_test

// A cycle refusal explains each edge by the contracts the next module on the
// cycle answers. These cases hold the explanation to the declarations: no
// reason names a supplier that does not provide or contribute what the
// consumer declared, and no edge of the cycle is left without its reason.

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
	paymentcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/payment/contracts"
)

func TestACycleNamesOnlyTheContractsItsNextModuleAnswers(t *testing.T) {
	build := func(*pkit.Wiring) (module.Module, error) {
		t.Fatal("a cycle must be refused before any module builds")
		return module.Module{}, nil
	}
	// taker takes every extension, which only outsider contributes: that is an
	// edge to outsider, never a reason on the edge to charger.
	taker := pkit.NewModule("taker", build,
		pkit.Provides[cartcontracts.Service](), pkit.Needs[paymentcontracts.Provider](),
		pkit.Needs[[]cartcontracts.Extension]())
	charger := pkit.NewModule("charger", build,
		pkit.Provides[paymentcontracts.Provider](), pkit.Needs[cartcontracts.Service]())
	outsider := pkit.NewModule("outsider", build, pkit.Contributes[cartcontracts.Extension]())

	err := pkit.NewApp("collect").Use(taker, charger, outsider).Validate(dev)
	want := "pkit: collect: Use: taker → charger → taker is a cycle: " +
		"taker needs paymentcontracts.Provider from charger; charger needs cartcontracts.Service from taker"
	if err == nil || err.Error() != want {
		t.Fatalf("Validate = %v, want %q", err, want)
	}
}

type (
	cycleScalarA struct{}
	cycleScalarB struct{}
	cycleScalarC struct{}
	cycleExtraX  struct{}
	cycleExtraY  struct{}
)

// cycleDecl is one declaration of a generated module, kept beside the pkit
// value so the test can say what the module declared.
type cycleDecl struct {
	kind     string // provides, needs, optional, contributes
	contract string
	many     bool
	decl     pkit.Declaration
}

func cycleDeclarations() []cycleDecl {
	scalar := func(name string, provides, needs, optional pkit.Declaration) []cycleDecl {
		return []cycleDecl{
			{"provides", name, false, provides},
			{"needs", name, false, needs},
			{"optional", name, false, optional},
		}
	}
	extra := func(name string, contributes, needs, optional pkit.Declaration) []cycleDecl {
		return []cycleDecl{
			{"contributes", name, false, contributes},
			{"needs", name, true, needs},
			{"optional", name, true, optional},
		}
	}
	var all []cycleDecl
	all = append(all, scalar("pkit_test.cycleScalarA", pkit.Provides[cycleScalarA](), pkit.Needs[cycleScalarA](), pkit.Optional[cycleScalarA]())...)
	all = append(all, scalar("pkit_test.cycleScalarB", pkit.Provides[cycleScalarB](), pkit.Needs[cycleScalarB](), pkit.Optional[cycleScalarB]())...)
	all = append(all, scalar("pkit_test.cycleScalarC", pkit.Provides[cycleScalarC](), pkit.Needs[cycleScalarC](), pkit.Optional[cycleScalarC]())...)
	all = append(all, extra("pkit_test.cycleExtraX", pkit.Contributes[cycleExtraX](), pkit.Needs[[]cycleExtraX](), pkit.Optional[[]cycleExtraX]())...)
	all = append(all, extra("pkit_test.cycleExtraY", pkit.Contributes[cycleExtraY](), pkit.Needs[[]cycleExtraY](), pkit.Optional[[]cycleExtraY]())...)
	return all
}

func TestEveryCycleReasonIsADeclarationItsNextModuleAnswers(t *testing.T) {
	pool := cycleDeclarations()
	rng := rand.New(rand.NewPCG(5790, 1))
	cycles := 0
	for range 20000 {
		n := 2 + rng.IntN(4)
		declared := map[string][]cycleDecl{}
		var mods []*pkit.Module
		for i := range n {
			name := fmt.Sprintf("m%d", i)
			var ds []cycleDecl
			for range 1 + rng.IntN(4) {
				ds = append(ds, pool[rng.IntN(len(pool))])
			}
			declared[name] = ds
			var decls []pkit.Declaration
			for _, d := range ds {
				decls = append(decls, d.decl)
			}
			mods = append(mods, pkit.NewModule(name, func(*pkit.Wiring) (module.Module, error) {
				return module.Module{Name: name}, nil
			}, decls...))
		}
		err := pkit.NewApp("cyc").Use(mods...).Validate(dev)
		if err == nil {
			continue
		}
		for line := range strings.SplitSeq(err.Error(), "\n") {
			if !strings.Contains(line, " is a cycle: ") {
				continue
			}
			cycles++
			checkCycleLine(t, line, declared)
		}
	}
	if cycles < 100 {
		t.Fatalf("only %d generated compositions were refused as cycles; the generator no longer reaches the case", cycles)
	}
}

// checkCycleLine fails unless every edge of the refused cycle says exactly
// the declarations of its consumer that its supplier answers: each one, in
// declaration order, once, and nothing the supplier does not answer.
func checkCycleLine(t *testing.T, line string, declared map[string][]cycleDecl) {
	t.Helper()
	body, ok := strings.CutPrefix(line, "pkit: cyc: Use: ")
	if !ok {
		t.Fatalf("cycle refusal %q is not a Use sentence", line)
	}
	pathText, reasonsText, _ := strings.Cut(body, " is a cycle: ")
	path := strings.Split(pathText, " → ")
	edges := strings.Split(reasonsText, "; ")
	if len(path) < 3 || path[0] != path[len(path)-1] || len(edges) != len(path)-1 {
		t.Fatalf("cycle refusal %q: path %q and %d edge explanations do not agree", line, path, len(edges))
	}
	for i, edge := range edges {
		want := answeredReasons(declared, path[i], path[i+1])
		if len(want) == 0 {
			t.Fatalf("cycle refusal %q names the edge %s → %s, which no declaration supports\n%v",
				line, path[i], path[i+1], declared)
		}
		if edge != strings.Join(want, ", ") {
			t.Fatalf("cycle refusal %q explains %s → %s as %q, want %q\n%v",
				line, path[i], path[i+1], edge, strings.Join(want, ", "), declared)
		}
	}
}

// answeredReasons is what the edge consumer → supplier must say: each needs or
// optional declaration of the consumer the supplier provides or contributes,
// in declaration order, a repeated sentence said once.
func answeredReasons(declared map[string][]cycleDecl, consumer, supplier string) []string {
	answers := func(contract string, many bool) bool {
		for _, d := range declared[supplier] {
			if d.contract == contract && (d.kind == "contributes" || !many && d.kind == "provides") {
				return true
			}
		}
		return false
	}
	var out []string
	for _, d := range declared[consumer] {
		if (d.kind != "needs" && d.kind != "optional") || !answers(d.contract, d.many) {
			continue
		}
		verb := "needs"
		switch {
		case d.many:
			verb = "takes"
		case d.kind == "optional":
			verb = "uses"
		}
		reason := fmt.Sprintf("%s %s %s from %s", consumer, verb, d.contract, supplier)
		if !slices.Contains(out, reason) {
			out = append(out, reason)
		}
	}
	return out
}
