package rest_test

// A command's guard is the fact and the surface follows it: commandRoute puts an
// operator grant on the control-plane router whatever resource the command acts on.
// Since Options.Auth is a spelling of that guard rather than a refusal, the field a
// guard is written in may not decide which router it is served from — a lift that
// read only CommandSpec.Auth would mount the installation's route on a customer's
// host while still recording the operator guard, so the document, the catalogue and
// the router would disagree about one command. Every spelling of one operator guard
// is compared here against the others: the address, the guard the middleware and the
// OpenAPI document both read, the recorded command, and the answer at a customer's
// own address. A declaration whose two fields differ only by the plan feature they
// name answers "who may run this" twice, and is refused before a route exists.

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// operatorGuard is the guard every spelling below states: the installation's work,
// not the tenant's, so the route it decides belongs to the operator's router.
var operatorGuard = httpx.OperatorPermission("task:write")

func guardSpelled(top, inOptions httpx.Auth) rest.CommandSpec[publishBody] {
	return rest.CommandSpec[publishBody]{
		Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
		Auth: top, Options: rest.CommandOptions{Auth: inOptions},
	}
}

// oneWay is the operation and the recorded command one spelling produced, with the
// two closures cleared, since a second mount is entitled to its own copies of those.
type oneWay struct {
	op      any
	command httpx.Command
}

func TestACommandGuardStatedOnlyInTheDeprecatedFieldStillPutsItOnTheOperatorsRouter(t *testing.T) {
	nothing := httpx.Auth{}
	mounted := map[string]oneWay{}
	for _, spelled := range []struct {
		name       string
		cmd        rest.CommandSpec[publishBody]
		positional bool
	}{
		{name: "in the options the positional form used", cmd: guardSpelled(nothing, operatorGuard)},
		{name: "at the top level", cmd: guardSpelled(operatorGuard, nothing)},
		{name: "in both fields, answering the same", cmd: guardSpelled(operatorGuard, operatorGuard)},
		{
			name:       "as the positional form still in use takes it",
			positional: true,
		},
	} {
		t.Run(spelled.name, func(t *testing.T) {
			var api *httpx.API
			var router http.Handler
			if spelled.positional {
				// rest.Command is handed its guard inside CommandOptions and lifts it
				// to where a CommandSpec keeps it: the lift may not lose the operator
				// half, because that half decides a router.
				api, router, _ = mountAs(t, spec, caller{}, func(api *httpx.API) {
					rest.Command(api.Surfaces(spec.Module), spec, "publish", "Publish a note",
						"Makes the note visible to the tenant.", nil, publishesItsOwnRow,
						rest.CommandOptions{Auth: operatorGuard})
				})
			} else {
				api, router, _ = mountAs(t, spec, caller{}, func(api *httpx.API) {
					rest.Mount(api.Surfaces(spec.Module), spec, spelled.cmd, publishesItsOwnRow)
				})
			}
			op := recorded(t, api, "tasks-task-publish")
			if op == nil {
				t.Fatal("the command mounted no route")
			}
			// The guard names the operator's grant, so the route is the operator's,
			// whichever field the author wrote the guard in.
			const at = "/api/v1/ops/tasks/task/{id}/publish"
			if op.Path != at {
				t.Errorf("the route is mounted at %s, want %s: the guard is an operator grant", op.Path, at)
			}
			declared, ok := op.Extensions[httpx.AuthExtension].(httpx.Auth)
			if !ok || declared.String() != "operator_permission task:write" {
				t.Errorf("the document says %v, want the guard the route enforces", op.Extensions[httpx.AuthExtension])
			}
			commands := api.Resources()[0].Commands
			if len(commands) != 1 {
				t.Fatalf("the resource records %d commands, want the one", len(commands))
			}
			command := commands[0]
			if command.Auth.String() != "operator_permission task:write" || command.Endpoint != at {
				t.Errorf("the catalogue offers the command at %s guarded by %s, want %s",
					command.Endpoint, command.Auth.String(), at)
			}
			// The customer's host keeps no copy of this door: the address the row route
			// would have had at the workspace surface answers nothing.
			if code, body := call(t, router, http.MethodPost,
				"/api/v1/tasks/task/"+uuid.New().String()+"/publish", `{"note":"written at the wrong host"}`); code != http.StatusNotFound {
				t.Errorf("POST the workspace address = %d %s, want %d", code, body, http.StatusNotFound)
			}
			command.Execute, command.Run = nil, nil
			mounted[spelled.name] = oneWay{op: *op, command: command}
		})
	}
	first := mounted["at the top level"]
	for name, shape := range mounted {
		if !reflect.DeepEqual(shape.op, first.op) || !reflect.DeepEqual(shape.command, first.command) {
			t.Errorf("the guard spelled %s mounted a different command from the guard at the top level:\n%#v\nagainst\n%#v",
				name, shape, first)
		}
	}
}

// TestACommandGuardWhoseTwoFieldsDifferOnlyByAPlanFeatureRefusesToMount is the
// agreement half of the rule: a permission stated beside the same permission asking
// for a plan feature first answers "who may run this" twice, and the mount cannot
// tell which answer the author meant. Only the feature differs, so this is the case
// a comparison of kinds and permissions alone would let through.
func TestACommandGuardWhoseTwoFieldsDifferOnlyByAPlanFeatureRefusesToMount(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("the command mounted; want a refusal naming the guard stated twice")
			return
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, "states its guard twice") {
			t.Errorf("the mount site says %q, want it to name the guard stated twice", msg)
		}
	}()
	_, _, _ = mountAs(t, spec, caller{}, func(api *httpx.API) {
		rest.Mount(api.Surfaces(spec.Module), spec, guardSpelled(
			httpx.Permission("task:write"),
			httpx.Permission("task:write").Needing("projects"),
		), publishesItsOwnRow)
	})
}
