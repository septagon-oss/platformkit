package rest_test

// declared_plan_feature_test.go holds the two answers a declared plan feature
// gives that no other file asks for, and the write address a Spec names.
//
// `kit/httpx` asks a declaration's questions in one place — `mayDeclare`, which
// the route's middleware, the six resource closures, `Readable`/`Writable` and
// `CommandsFor` all call — so what a case has to pin is each answer that place
// gives: an Entitler that cannot decide is an outage at a closure exactly as it
// is at the route and never a silent yes; the write door closes with the plan as
// the read one does, and takes a command guarded by the same declaration with it.
// The third case reads the projection the catalogue and the refusal both quote:
// which address a resource names as its write door is a fact about `Operations`
// and not about which router the Spec was handed.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// planFaulty is the outage arm: a plan that could not be read is not a plan that
// excludes, and a door has to say so rather than open.
type planFaulty struct{}

func (planFaulty) Includes(context.Context, tenancy.Tenant, string) (bool, error) {
	return false, errors.New("billing is not answering")
}

// featureWriteSpec is the Spec whose *writes* are the ones behind a plan feature:
// a price list a tenant reads as part of the plan and only the installation
// changes.
func featureWriteSpec() rest.Spec[*Task] {
	s := spec
	s.Write = ""
	s.WriteAuth = httpx.Permission("task:write").Needing("projects")
	return s
}

// askedOfTheWriteDoor is what one request learned of the write half: the route's
// answer, the door's, and the commands the entry would offer.
type askedOfTheWriteDoor struct {
	routeStatus  int
	writable     bool
	commandVerbs []string
	writeErr     error
}

func askTheWriteDoor(t *testing.T, plan httpx.Entitler) askedOfTheWriteDoor {
	t.Helper()
	s := featureWriteSpec()
	api, router, admin := mountEntitled(t, s, plan)
	rest.Command[struct{}](api.Surfaces(s.Module), s, "archive", "Archive a task",
		"Guarded by the Spec's own write declaration, so by the plan as well.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) {
			return nil, nil
		}, rest.CommandOptions{})
	id := seed(t, admin, "a row behind the write door")
	res := api.Resources()[0]
	var out askedOfTheWriteDoor
	httpx.Register(api.Surfaces(s.Module).App, probeOperation("write-door-probe", "/write-door-probe"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			out.writable = res.Writable(ctx)
			for _, c := range res.CommandsFor(ctx) {
				out.commandVerbs = append(out.commandVerbs, c.Verb)
			}
			_, out.writeErr = res.Update(ctx, uuid.New(), map[string]any{"title": "written past the plan"})
			return nil, nil
		})
	out.routeStatus, _ = call(t, router, http.MethodPatch, "/api/v1/tasks/task/"+id, `{"title":"x"}`)
	if status, body := call(t, router, http.MethodPost,
		api.Surfaces(s.Module).App.Path("/write-door-probe"), ""); status != http.StatusNoContent {
		t.Fatalf("the probe route = %d %s, want %d: this case asks about the write doors",
			status, head(body), http.StatusNoContent)
	}
	return out
}

// TestTheWriteDoorOfAGuardNamingAPlanFeatureClosesWithThePlan holds the write
// half of a declaration to the same answer as its read half and as its route. A
// caller whose plan lacks the feature is answered 402 at the PATCH, so the same
// caller's `Writable` says no — which is what keeps an Edit door undrawn and the
// catalogue's `write_path` unprinted — and the command the Spec mounted under
// its own write declaration is not offered either. With the feature in the plan
// all three answers open, which is what makes the refusals above about the plan.
func TestTheWriteDoorOfAGuardNamingAPlanFeatureClosesWithThePlan(t *testing.T) {
	behind := askTheWriteDoor(t, planWithout{"projects"})
	if behind.routeStatus != http.StatusPaymentRequired {
		t.Fatalf("PATCH a row = %d, want %d: the route asks the plan this Spec declared",
			behind.routeStatus, http.StatusPaymentRequired)
	}
	if behind.writable {
		t.Errorf("Resource.Writable answered yes for a tenant the write route answers %d: the page's door and the catalogue promise a write nothing answers",
			http.StatusPaymentRequired)
	}
	if len(behind.commandVerbs) != 0 {
		t.Errorf("CommandsFor offers %v to a tenant whose plan lacks the feature the write declaration names",
			behind.commandVerbs)
	}
	if behind.writeErr == nil {
		t.Error("Resource.Update wrote for a tenant whose plan lacks the feature its write guard names")
	} else if !refused(behind.writeErr, http.StatusPaymentRequired) {
		t.Errorf("Resource.Update = %v, want the refusal its route answers", behind.writeErr)
	}

	granted := askTheWriteDoor(t, planEvery{})
	if granted.routeStatus == http.StatusPaymentRequired {
		t.Fatalf("PATCH a row with the feature in the plan = %d, want the write reached", granted.routeStatus)
	}
	if !granted.writable {
		t.Error("Writable answered no for a caller the write guard admits")
	}
	if len(granted.commandVerbs) != 1 || granted.commandVerbs[0] != "archive" {
		t.Errorf("CommandsFor = %v, want the one command, with the feature in the plan", granted.commandVerbs)
	}
	if granted.writeErr != nil && !refused(granted.writeErr, http.StatusNotFound) {
		t.Errorf("Resource.Update with the feature in the plan = %v, want the row's own 404 and not a guard's refusal",
			granted.writeErr)
	}
}

// TestAPlanThatCannotBeReadRefusesTheReadItGuardsAndNotTheWriteItDoesNot is the
// outage arm. `entitled` refuses 503 rather than let billing being unreachable
// read as a plan that excludes, and the closures reach that same decision, so
// they refuse 503 too. A closure that read "I could not tell" as "yes" would be
// the one door that opens widest when the installation is at its worst.
func TestAPlanThatCannotBeReadRefusesTheReadItGuardsAndNotTheWriteItDoesNot(t *testing.T) {
	api, router, admin := mountEntitled(t, featureGuardSpec(), planFaulty{})
	seed(t, admin, "a row behind an unreadable plan")
	res := api.Resources()[0]
	var (
		readable, writable bool
		listErr            error
	)
	httpx.Register(api.Surfaces("tasks").App, probeOperation("plan-outage-probe", "/plan-outage-probe"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			readable, writable = res.Readable(ctx), res.Writable(ctx)
			_, _, listErr = res.List(ctx, crud.Query{Limit: 10})
			return nil, nil
		})
	if status, body := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); status != http.StatusServiceUnavailable {
		t.Fatalf("GET the collection = %d %s, want %d: an Entitler that cannot decide is an outage",
			status, head(body), http.StatusServiceUnavailable)
	}
	if status, body := call(t, router, http.MethodPost,
		api.Surfaces("tasks").App.Path("/plan-outage-probe"), ""); status != http.StatusNoContent {
		t.Fatalf("the probe route = %d %s, want %d", status, head(body), http.StatusNoContent)
	}
	if readable {
		t.Error("Readable answered yes for a tenant whose plan could not be read: an outage is not an entitlement")
	}
	if !writable {
		// The write declaration of this Spec names no feature, so the outage is not
		// its question: what a plan gate refuses is what the declaration named.
		t.Error("Writable answered no though this Spec's write declaration names no plan feature")
	}
	if listErr == nil {
		t.Error("Resource.List answered rows for a tenant whose plan could not be read")
	} else if !refused(listErr, http.StatusServiceUnavailable) {
		t.Errorf("Resource.List = %v, want the 503 its route answers", listErr)
	}
}
