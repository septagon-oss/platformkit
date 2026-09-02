// Package task is the module manifest: what the kernel needs to know about
// tasks that it cannot learn from a function call.
//
// It is the shape every module follows: contracts/, internal/, and one exported
// function taking a struct of typed dependencies and returning a module.Module.
// main constructs it in dependency order and the compiler checks the wiring,
// which is the whole of docs/adr/0002 — a dependency this module needs and main
// did not pass is a compile error at the construction site, not a nil at boot.
package task

import (
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// Deps is what this module cannot make for itself: a struct rather than a
// parameter list, so adding one is a named field at every call site rather than
// a fourth positional argument. Every module follows this, and an empty Deps is
// a module that needs nothing.
type Deps struct {
	// Tenants is how the SLA sweep reaches every tenant. The tenant module
	// implements it in E3; until then apps/platformkit/dev.go does.
	Tenants jobs.TenantLister

	// SweepEvery is how often the sweep runs; zero means a minute. The promise
	// this module keeps is measured in hours, so a minute of lag on noticing a
	// breach is free and anything faster is a query per tenant per tick for
	// nobody's benefit. A test sets it lower.
	SweepEvery time.Duration
}

// sweepEvery is the default interval. See Deps.SweepEvery.
const sweepEvery = time.Minute

// spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads. Everything
// generic about a task is this value; internal/ holds only what is not.
var spec = crud.Spec[*contracts.Task]{
	Module:     "task",
	Entity:     "task",
	Path:       "/api/v1/task/tasks",
	Read:       contracts.PermissionTaskRead,
	Write:      contracts.PermissionTaskUpdate,
	SoftDelete: true,
}

// Module is the manifest. The implementation is constructed here, in one line,
// and handed to the two places that use it, so this module's own wiring is
// visible in the file that declares it.
func Module(deps Deps) module.Module {
	svc := internal.NewService()
	every := deps.SweepEvery
	if every == 0 {
		every = sweepEvery
	}
	// The one thing about this entity that is not generic on the way in: a task
	// created with a deadline already behind it is breached on arrival, in the
	// create's own transaction, rather than a minute later when the sweep gets
	// to it. The hook is set here and not in the spec literal above because it
	// needs the service, and the service is constructed here.
	mounted := spec
	mounted.AfterCreate = internal.BreachOnArrival(svc)
	return module.Module{
		Name:        "task",
		Permissions: contracts.Permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Tasks", Path: "/admin/task/tasks", Permission: contracts.PermissionTaskRead},
		},
		Jobs: []jobs.Job{internal.SLASweep(deps.Tenants, svc, every)},
		// Written out so the absence is a decision: a task is raised by whoever
		// raises it, and this module has no opinion about anybody else's events.
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			mounted.Mount(api)
			internal.RegisterRoutes(api, svc, mounted.Path)
		},
	}
}
