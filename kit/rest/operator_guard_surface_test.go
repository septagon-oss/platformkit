package rest_test

// operator_guard_surface_test.go is the one guard spelling the shorthand cannot
// produce: an operator grant *and* the plan feature it belongs to. The refusal
// table sends a bare httpx.OperatorPermission back to Read/OperatorRead — where
// kit/app cross-checks the operator flag against the module manifest — and that
// advice is right only while the shorthand can say what the author means. A
// feature is exactly what it cannot say, so this spelling mounts, and the thing
// it then decides is which router a tenant's rows are served from.
//
// Nothing in the suite named this combination until now: the operator half of
// the refusal table is tested (`TestAnOperatorPermissionDeclarationNamesBoth
//Fields`), the feature half is tested with a plain permission
// (`TestAPlainPermissionInReadAuthRefusesToMountAndOneNeedingAFeatureDoesNot`),
// and the address the two together produce is tested by neither. The surface gate
// accepts an operator declaration on the App router (kit/httpx/surfaces.go's
// `accepted` answers yes for the whole App case), so a Spec that lost its way to
// surfaces.Ops would boot, answer at a customer's host, and refuse there — the
// guard would hold and the address would be the installation's control plane
// served from the workspace. That is what these assertions are for.

import (
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestAnOperatorGuardThatAlsoNamesAPlanFeatureKeepsItsRoutesOnTheOperatorsRouter(t *testing.T) {
	s := spec
	s.Read = ""
	s.ReadAuth = httpx.OperatorPermission("task:read").Needing("projects")
	api, router, _ := mountPlain(t, s, caller{})

	// It mounts at all. The bare operator declaration is refused; this one, which
	// the two shorthand fields cannot express, is not.
	want := map[string]string{
		"tasks-task-list":   "/api/v1/ops/tasks/task",
		"tasks-task-read":   "/api/v1/ops/tasks/task/{id}",
		"tasks-task-create": "/api/v1/tasks/task",
		"tasks-task-update": "/api/v1/tasks/task/{id}",
	}
	for id, at := range want {
		op := recorded(t, api, id)
		if op == nil {
			t.Fatalf("%s is not mounted; the Spec named no rest.Operations, so all five are offered", id)
		}
		if op.Path != at {
			t.Errorf("%s is mounted at %s, want %s: the read guard names the operator's grant, so its routes belong to the operator's router and the write guard does not", id, op.Path, at)
		}
	}
	// The same truth at the address a person would knock on: the collection
	// address of a customer's workspace no longer answers a read at all, because
	// the only verb left there is the create the Spec did not make operator work.
	if code, _ := call(t, router, http.MethodGet, "/api/v1/tasks/task", ""); code != http.StatusMethodNotAllowed {
		t.Errorf("GET the workspace collection address = %d, want %d: the read this Spec guards as operator work must not be served there", code, http.StatusMethodNotAllowed)
	}
}
