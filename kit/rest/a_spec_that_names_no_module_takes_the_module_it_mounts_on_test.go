package rest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// taskSpec is a resource whose module half is left blank, which is what the
// kernel fills: the module a resource lives in is the module whose router it
// mounts on, and a manifest that has nothing to add about its own name should be
// able to say so by writing nothing.
func taskSpec() rest.Spec[*Task] {
	return rest.Spec[*Task]{
		Entity: "task", Path: "/tasks", Read: "task:read", Write: "task:write",
		Operations: []httpx.CRUD{httpx.CRUDList, httpx.CRUDCreate},
	}
}

// TestASpecThatNamesNoModuleTakesTheModuleItMountsOn asks the fill of everything
// the module name reaches: the operation id, the document's tag, the catalog
// entry and the event names the mount declares on the write operations.
func TestASpecThatNamesNoModuleTakesTheModuleItMountsOn(t *testing.T) {
	api, _ := patchDocumentAPI()
	surfaces := api.Surfaces("task")
	taskSpec().Mount(surfaces)

	ids, tags, published := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, op := range api.Recorded() {
		ids[op.OperationID] = true
		for _, tag := range op.Tags {
			tags[tag] = true
		}
		if names, ok := op.Extensions[httpx.EventsExtension].([]string); ok {
			for _, name := range names {
				published[name] = true
			}
		}
	}
	for _, want := range []string{"task-task-list", "task-task-create"} {
		if !ids[want] {
			t.Errorf("no operation is named %q; the ids are %v", want, ids)
		}
	}
	if !tags["task"] {
		t.Errorf("the document tags nothing as the task module: %v", tags)
	}
	resources := api.Resources()
	if len(resources) != 1 || resources[0].Module != "task" {
		t.Fatalf("the catalog carries %v, want one resource in the task module", resources)
	}
	if !published["task.task.created"] {
		t.Errorf("the create operation publishes %v, want an event inside task's namespace", published)
	}
}

func TestASpecNamingAnotherModulesModuleIsRefusedAtMount(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("a Spec filed under another module's name mounted")
		}
		message, _ := p.(string)
		for _, want := range []string{"billing", "task", "the module whose manifest declares it"} {
			if !strings.Contains(message, want) {
				t.Errorf("the refusal does not name %q: %s", want, message)
			}
		}
	}()
	borrowed := taskSpec()
	borrowed.Module = "billing"
	api, _ := patchDocumentAPI()
	borrowed.Mount(api.Surfaces("task"))
}

// TestASpecThatBuildsItsEventNamesBeforeMountNamesItsModule is the half the
// kernel cannot fill: a manifest asks a Spec for its event names while the
// manifest is being built, which is before any router exists to say which module
// this is. A name built from a module the kernel would fill later reads
// ".task.created", and the app gate would refuse the route that published the
// real one.
func TestASpecThatBuildsItsEventNamesBeforeMountNamesItsModule(t *testing.T) {
	unnamed := taskSpec()
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("Event answered for a Spec that names no module")
		}
		message, _ := p.(string)
		for _, want := range []string{"task", "Module"} {
			if !strings.Contains(message, want) {
				t.Errorf("the refusal does not name %q: %s", want, message)
			}
		}
	}()
	unnamed.Event("created")
}

func TestASpecNamingItsModuleNamesItsEvents(t *testing.T) {
	named := taskSpec()
	named.Module = "task"
	if got, want := named.Event(rest.Created), "task.task.created"; got != want {
		t.Errorf("Event = %q, want %q", got, want)
	}
	if got := named.Events(); len(got) != 3 || got[0] != "task.task.created" {
		t.Errorf("Events = %v, want the three names inside task's namespace", got)
	}
}
