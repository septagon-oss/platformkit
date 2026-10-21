package pkit_test

// A build that reads a contract it never declared stops at that read. The
// module is refused by name, with the contract and the fact that nothing
// declared it; the module that needed what it provides is suppressed and owes
// no sentence; an unrelated module's own refusal is still collected; and the
// caller hears all of it as one CompositionError, never as a panic.

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
)

type heldPort interface{ Hold() }

type readPort interface{ Read() }

type midPort interface{ Hold() }

type holder struct{}

func (holder) Hold() {}

var errOther = errors.New("other refuses on its own")

func TestAnUndeclaredReadFailsOnlyItsModule(t *testing.T) {
	ran := false
	app := undeclaredReadApp(&ran)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the failed read reached the caller as a panic: %v", r)
		}
	}()
	err := app.Validate(dev)
	ce := compositionError(t, err)
	want := []pkit.DescribedProblem{
		{Cause: "Build", Module: "reader", Contract: "pkit_test.readPort", Requiredness: "undeclared",
			Sentence: "reader reads pkit_test.readPort, which it did not declare with Needs or Optional"},
		{Cause: "Build", Module: "other", Sentence: "other: other refuses on its own"},
	}
	if !reflect.DeepEqual(ce.Problems, want) {
		t.Errorf("the problems are %+v, want %+v", ce.Problems, want)
	}
	if !reflect.DeepEqual(ce.Suppressed, []pkit.DescribedSuppression{{Module: "dependent", By: "reader"}}) {
		t.Errorf("the suppressed modules are %+v, want dependent, by reader", ce.Suppressed)
	}
	if ran {
		t.Error("the module that needs the refused module's contract was built")
	}
	if lines := strings.Count(err.Error(), "\n") + 1; lines != 2 || strings.Contains(err.Error(), "dependent") {
		t.Errorf("want two sentences and no word about the suppressed module:\n%v", err)
	}
	if !errors.Is(err, errOther) {
		t.Errorf("errors.Is no longer reaches the error a build returned: %v", err)
	}
}

// undeclaredReadApp is user.Module plus a reader whose build reads a contract it
// never declared, a module that needs what the reader provides, and a module
// with a refusal of its own.
func undeclaredReadApp(ran *bool) *pkit.App {
	reader := pkit.NewModule("reader", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[heldPort](w, holder{})
		pkit.Get[readPort](w).Read()
		return module.Module{Name: "reader"}, nil
	}, pkit.Provides[heldPort]())
	dependent := pkit.NewModule("dependent", func(w *pkit.Wiring) (module.Module, error) {
		*ran = true
		return module.Module{Name: "dependent"}, nil
	}, pkit.Needs[heldPort]())
	other := pkit.NewModule("other", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{}, errOther
	})
	return pkit.NewApp("collect").Use(user.Module, reader, dependent, other)
}

// halfModule provides heldPort, puts it, and then refuses: the provider whose
// partial output must not be read by anything after it.
func halfModule() *pkit.Module {
	return pkit.NewModule("half", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[heldPort](w, holder{})
		return module.Module{}, errBoom
	}, pkit.Provides[heldPort]())
}

// compositionError is the CompositionError behind err, or a failed test.
func compositionError(t *testing.T, err error) *pkit.CompositionError {
	t.Helper()
	var ce *pkit.CompositionError
	if !errors.As(err, &ce) {
		t.Fatalf("the refusal is not a CompositionError: %v", err)
	}
	return ce
}
