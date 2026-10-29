package task_test

// The task module hands out four contracts of one service, and a composition
// reads them the way it reads any other provided contract: by type, from the
// plan. What these cases pin is the two halves of that promise — that an app
// using task.Module is answered for all four and gets one object behind them,
// and that a module needing any of them without the module is refused the way a
// missing Service is refused, so the supply can never become conditional.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// deskInputs answers the three ports no composition can be planned without and
// the tenant lister this module Needs, with the same two yes-answering helpers
// the mounted-route cases use. It is the smallest app that can hold a task desk.
func deskInputs() *pkit.Module {
	return pkit.NewModule("inputs", func(w *pkit.Wiring) (module.Module, error) {
		who := caller{}
		pkit.Put[httpx.TenantLoader](w, who)
		pkit.Put[httpx.Authorizer](w, who)
		pkit.Put[pkit.Authenticator](w, func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		})
		pkit.Put[jobs.TenantLister](w, activeTenants{acme})
		return module.Module{Name: "inputs"}, nil
	}, pkit.Provides[httpx.TenantLoader](), pkit.Provides[httpx.Authorizer](),
		pkit.Provides[pkit.Authenticator](), pkit.Provides[jobs.TenantLister]())
}

// needsDesk is a consumer of the desk: a module that asks the plan for a part of
// it rather than building one, and records what it was handed so the case can
// compare that with what the plan answers.
type needsDesk struct {
	creator taskcontracts.Creator
	reader  taskcontracts.Reader
	locked  taskcontracts.LockedReader
}

func (n *needsDesk) module() *pkit.Module {
	return pkit.NewModule("deskconsumer", func(w *pkit.Wiring) (module.Module, error) {
		n.creator = pkit.Get[taskcontracts.Creator](w)
		n.reader = pkit.Get[taskcontracts.Reader](w)
		n.locked = pkit.Get[taskcontracts.LockedReader](w)
		return module.Module{Name: "deskconsumer"}, nil
	}, pkit.Needs[taskcontracts.Creator](), pkit.Needs[taskcontracts.Reader](),
		pkit.Needs[taskcontracts.LockedReader]())
}

func TestUsingTheTaskModuleGivesItsCreatorReaderAndLockedReader(t *testing.T) {
	consumer := &needsDesk{}
	plan, err := pkit.NewApp("desk").Use(deskInputs(), task.Module, consumer.module()).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatalf("a composition that uses task.Module was refused: %v", err)
	}

	svc, ok := pkit.Value[taskcontracts.Service](plan)
	if !ok || svc == nil {
		t.Fatal("the plan answers no taskcontracts.Service")
	}
	// The plan is read four ways and the consumer's own reads are compared with it:
	// four values under four keys would also pass a test that only asked each key
	// once, and would be four desks in one composition. A consumer declares the
	// reads it makes, so it asks for these three and not for the whole service.
	for _, one := range []struct {
		contract string
		got      any
		taken    any
	}{
		{"taskcontracts.Creator", mustValue[taskcontracts.Creator](t, plan), any(consumer.creator)},
		{"taskcontracts.Reader", mustValue[taskcontracts.Reader](t, plan), any(consumer.reader)},
		{"taskcontracts.LockedReader", mustValue[taskcontracts.LockedReader](t, plan), any(consumer.locked)},
	} {
		if one.got == nil {
			t.Errorf("the plan answers %s with nothing", one.contract)
			continue
		}
		if any(one.got) != any(svc) {
			t.Errorf("the plan's %s is %v, not the taskcontracts.Service it is", one.contract, one.got)
		}
		if one.taken != any(svc) {
			t.Errorf("the consumer's %s read is %v, not the taskcontracts.Service the routes write through", one.contract, one.taken)
		}
	}

	// Writer stays unprovided, so its absence is a decision somebody would see as a
	// diff: it is the one door that writes a protected field with no gate before it.
	if _, ok := pkit.Value[taskcontracts.Writer](plan); ok {
		t.Error("the plan answers taskcontracts.Writer, which this module keeps out of every composition on purpose")
	}
}

func TestNeedingTheTaskReadWithoutTheTaskModuleIsRefused(t *testing.T) {
	_, err := pkit.NewApp("desk").Use(deskInputs(), (&needsDesk{}).module()).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err == nil {
		t.Fatal("a composition that needs the task contracts without task.Module planned")
	}
	// The sentence names the module to add, so the refusal is correctable by reading
	// it: the same shape a missing Service is refused with, and for the same reason.
	msg := err.Error()
	for _, want := range []string{
		"deskconsumer needs taskcontracts.Creator: add task.Module to desk",
		"deskconsumer needs taskcontracts.Reader: add task.Module to desk",
		"deskconsumer needs taskcontracts.LockedReader: add task.Module to desk",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, msg)
		}
	}
}

// mustValue fails the case for a contract the plan should answer, so a comparison
// at the call site always compares two values and never a nil with a value.
func mustValue[T any](t *testing.T, p *pkit.Planned) T {
	t.Helper()
	v, ok := pkit.Value[T](p)
	if !ok || any(v) == nil {
		t.Fatalf("the plan answers no %T", *new(T))
	}
	return v
}
