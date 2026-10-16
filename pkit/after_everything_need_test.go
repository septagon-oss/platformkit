package pkit_test

// An app may compose a module that runs after every other one and still needs
// a service: 0074's admin reads the whole composition and, in the reference
// registry, needs the console contract of its own. Whatever the resolver
// decides about that need, the composition owes an answer — either the need is
// supplied, or the composition is refused with a sentence naming the module and
// the contract. It may not be accepted with the need unfilled, and Explain may
// not crash on it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	usercontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/user/contracts"
)

func TestTheModuleThatRunsAfterEverythingIsAnsweredForWhatItNeeds(t *testing.T) {
	var got usercontracts.Service
	inspector := pkit.NewModule("inspector", func(w *pkit.Wiring) (module.Module, error) {
		got = pkit.Get[usercontracts.Service](w)
		return module.Module{Name: "inspector"}, nil
	}, pkit.Needs[usercontracts.Service](), pkit.After(pkit.Everything))

	app := pkit.NewApp("collect").Use(user.Module, inspector)

	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Validate panicked: %v", r)
			}
		}()
		return app.Validate(dev)
	}()
	if err != nil {
		// A refusal is an honest answer, provided it names the module and the
		// contract it cannot place.
		if !strings.Contains(err.Error(), "inspector") || !strings.Contains(err.Error(), "Service") {
			t.Errorf("the refusal names neither the module nor the contract: %v", err)
		}
	} else if got == nil {
		t.Errorf("collect accepted a composition whose required need usercontracts.Service was never supplied: inspector.Module built with the zero value")
	}

	txt, explainErr := func() (txt string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Explain panicked: %v", r)
			}
		}()
		s, e := app.Explain(dev)
		if e != nil {
			return "", e
		}
		return s, nil
	}()
	if explainErr != nil {
		// Explain may refuse the same composition Validate accepted nothing
		// else — but a refusal is an error value, not a crash.
		if strings.HasPrefix(explainErr.Error(), "Explain panicked:") {
			t.Fatalf("%v", explainErr)
		}
		return
	}
	if got != nil && !strings.Contains(txt, "inspector.Module needs usercontracts.Service from user.Module") {
		t.Errorf("the composition file does not say who supplies the need it resolved:\n%s", txt)
	}
}
