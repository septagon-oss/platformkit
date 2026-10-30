// Package task is the module manifest: what the kernel needs to know about
// tasks that it cannot learn from a function call.
//
// It is the shape every module follows: contracts/, internal/, and one exported
// function taking a struct of typed dependencies and returning a module.Module.
// The application constructs it in dependency order. Compilation checks the
// dependency types; composition tests exercise the values supplied.
package task

import (
	"context"
	"errors"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// Deps is what this module cannot make for itself: a struct rather than a
// parameter list, so adding one is a named field at every call site rather than
// a fourth positional argument. Every module follows this, and an empty Deps is
// a module that needs nothing.
type Deps struct {
	// Service shares one lifecycle implementation with product callers. A
	// policy-enabled composition supplies NewServiceWithPolicy here and hands
	// the same service to its other modules.
	Service contracts.Service

	// Policy optionally refines assignment and resolution for the locked task.
	// Existing route grants still apply. System SLA maintenance is independent.
	// Supply either Policy or a preconfigured Service, never both.
	Policy tenancy.Policy

	// Tenants is how the SLA sweep reaches active tenants. The application
	// supplies tenant/contracts.Active over its tenant service.
	Tenants jobs.TenantLister

	// SweepEvery is how often the sweep runs; zero means a minute. The promise
	// this module keeps is measured in hours, so a minute of lag on noticing a
	// breach is free and anything faster is a query per tenant per tick for
	// nobody's benefit. A test sets it lower.
	SweepEvery time.Duration

	// Gate is the door in front of the five routes this module mounts: asked once
	// the row is locked and the body merged, before anything is written, and never
	// at all when nothing is wired here. It answers whether these fields may be
	// written directly, which is not this module's question to answer — it owns the
	// task's invariants, and the answer belongs to an installation with an opinion
	// about which of its promises need a second pair of eyes.
	//
	// The reference application wires one over modules/change; a composition that
	// wires nothing writes tasks exactly as it always did. This is the shape
	// modules/site's Deps.Gate takes, one level out from the singleton's Save closure
	// it had to be written into there: a Spec mounts its own routes, so the door has
	// to be handed in rather than written around them.
	Gate rest.Gate
}

// sweepEvery is the default interval. See Deps.SweepEvery.
const sweepEvery = time.Minute

// Spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads. Everything
// generic about a task is this value; internal/ holds only what is not.
//
// It is exported for one consumer and one reason: a write made outside an HTTP
// request — the application's own seed — still owes this entity's validation,
// its list schema and its event, and the alternative was a second write core
// beside this one. Module below mounts a copy of it with one more hook.
var Spec = rest.Spec[*contracts.Task]{
	Module:     "task",
	Entity:     "task",
	Path:       "/tasks",
	Read:       contracts.PermissionTaskRead,
	Write:      contracts.PermissionTaskUpdate,
	SoftDelete: true,
	// The four fields a command owns. A PATCH that could set assigneeId would
	// make somebody responsible without moving the status and without
	// task.assigned; one that could set slaBreached would forge the fact the
	// SLA report counts. Each of the four has a route.
	// The one list of them, in the contracts package, which also decides what an
	// apply refuses and what the gate may name: one sentence, three doors.
	Immutable: contracts.CommandOwnedFields,
	// What the creator publishes beyond task.task.created, so the create operation
	// declares it and the boot gate can check it against Events below.
	HookEvents: []string{contracts.EventSLABreached},
	// Present is how a task reads to the person holding one: the words, the icon,
	// which field names a row and which say what waiting costs. Every word
	// here is English written by the author of the entity, and the copy key it is
	// served under is derived from this resource's own address — see
	// ui/resource/hints.go — which is why no `en.json` exists to keep it in step.
	Present: entity.EntryHints{
		Description:      "Set priorities, assign work and keep track of what is due.",
		EmptyDescription: "Create a task to give the work a clear next step.",
		Singular:         "task",
		Plural:           "tasks",
		Icon:             "task",
		Group:            &entity.ResourceGroup{Key: "work", Label: "Work"},
		// The row says its title, and its status; between them, what it costs to
		// wait, when it is due, and whether its response is already overdue.
		PrimaryField:  "title",
		PreviewField:  "description",
		StatusField:   "status",
		SummaryFields: []string{"priority", "dueAt", "slaBreached"},
		// The three sorts a desk actually runs: what it is called, what it is due,
		// what moved last. `updatedAt` is legal here although no row shows it —
		// `sortable` refuses a name pointing at a `hidden` field, and `detail` is
		// not `hidden`.
		Sortable: []string{"title", "dueAt", "updatedAt"},
		// The question a first screen asks about a list it cannot count by hand:
		// what is due today. It is the same rows the list pages through, narrowed
		// by a predicate and not kept in a second store of numbers, and the day is
		// resolved when the question is asked, so "today" is the caller's today and
		// not this binary's build date. The daily series below is the same kind of
		// question: the list's rows, grouped instead of narrowed.
		// createdAt is already the index a bounded window reads: tasks_tenant_created.
		TrendField: "createdAt",
		Highlight: &entity.EntryHighlight{
			Label: "Due today",
			Filter: []entity.EntryPredicate{
				{Field: "dueAt", Op: "gte", Value: "today"},
				{Field: "dueAt", Op: "lt", Value: "tomorrow"},
			},
		},
		// A person may keep a copy of a task. What this entity owns is the pair of
		// fields a kept copy quotes — its own id and its own write counter — and the
		// four words its control shows; which installation keeps copies at all, and
		// which pairs its service may capture, are the composition's, and `savable`
		// resolves to null where it composed none. The pair of slugs is the entity's
		// own address because a copy of a task is a copy of a task in every product
		// that composes this one.
		Saving: &entity.SavingHints{
			Module: "task", Kind: "task", KeyField: "id", VersionField: "revision",
			Words: entity.SavingWords{
				Save: "Save task", Saved: "Saved to your tasks",
				Remove: "Remove from saved", ListTitle: "My saved tasks",
			},
		},
	},
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is
// not here fails startup instead of denying everyone forever. It lives beside
// the manifest and not in contracts/ because it was the only thing there that
// needed kit/module.
var permissions = []module.Permission{
	{Key: contracts.PermissionTaskRead, Label: "read tasks"},
	{Key: contracts.PermissionTaskUpdate, Label: "write tasks"},
}

// NewService constructs task lifecycle commands for application composition.
// Pass the returned value through Deps.Service to share it with task routes.
func NewService() contracts.Service { return internal.NewService() }

// NewLockedReader returns the same task service Module builds, as the one interface
// a caller about to diff a task needs (see contracts.LockedReader for why that is a
// second interface and not a method on Service). It costs a second value because the
// service has no fields — everything it needs arrives with the transaction — so
// there is nothing here to keep in step with anything else.
func NewLockedReader() contracts.LockedReader { return internal.NewService() }

// NewWriter returns that service again, as the whole-row write an approved proposal
// comes through (contracts.Writer). The one door of its kind: the gate stands in
// front of the five routes and never here, because a gate on both sides is a wall —
// the approved change could be proposed and never applied. What makes it safe is that
// it is single, named, and audited, and a caller that reached for it outside an apply
// would be composition code, which is the trust boundary the tenant model already
// draws.
func NewWriter() contracts.Writer { return internal.NewService() }

// NewReader returns the plain read of a task by id (contracts.Reader), for a
// module that links to another module's task without locking it.
func NewReader() contracts.Reader { return internal.NewReader() }

// NewServiceWithPolicy adds resource decisions to assignment and resolution.
// The caller still owns route grants, current identity and tenant transactions.
// A nil policy is refused with a panic; NewServiceWithPolicyChecked answers it as
// an error.
func NewServiceWithPolicy(policy tenancy.Policy) contracts.Service {
	svc, err := NewServiceWithPolicyChecked(policy)
	if err != nil {
		panic(err.Error())
	}
	return svc
}

// NewServiceWithPolicyChecked answers a nil policy provider as the error
// NewServiceWithPolicy panics with: the point of the policy is to refuse a
// resource the caller does not hold, and a service with no policy answers that
// question by allowing everything it was asked.
func NewServiceWithPolicyChecked(policy tenancy.Policy) (contracts.Service, error) {
	if policy == nil {
		return nil, errors.New("task: a policy-enabled service requires a policy provider")
	}
	svc := internal.NewService()
	svc.Policy = policy
	return svc, nil
}

// roles are the two people the task desk is written for. They are declared here
// rather than listed by the application because they are the task capability's
// own: a coordinator runs the desk and an observer follows it, in every
// installation that composes this module, and the permissions that say so are the
// two above. It is what lets a test ask, generated, whether a granted role
// reaches the screens this module serves — a role written into an app's main
// cannot be checked against a module from the outside.
//
// Which task a coordinator may resolve is policy/task.rego's, not a grant's, and
// a product that wants a further role names it in its own composition.
var roles = []module.RoleDecl{
	{Name: "coordinator", Grants: []string{contracts.PermissionTaskRead, contracts.PermissionTaskUpdate}},
	{Name: "observer", Grants: []string{contracts.PermissionTaskRead}},
}

// Module mounts one shared lifecycle implementation for routes and jobs.
//
// Deprecated: use NewChecked, which answers this refusal as an error.
func New(deps Deps) module.Module {
	m, err := NewChecked(deps)
	if err != nil {
		panic(err.Error())
	}
	return m
}

// NewChecked mounts one shared lifecycle implementation for routes and jobs, and
// answers as an error the two ways of naming a policy at once: the shared service
// already carries one, and the Desk would then answer with the other. A zero
// SweepEvery keeps the interval beside it. It opens nothing, runs no query and is
// safe to call twice.
func NewChecked(deps Deps) (module.Module, error) {
	svc := deps.Service
	if deps.Policy != nil {
		if svc != nil {
			return module.Module{}, errors.New("task: configure policy on the shared service or supply Policy, not both")
		}
		policyService, err := NewServiceWithPolicyChecked(deps.Policy)
		if err != nil {
			return module.Module{}, err
		}
		svc = policyService
	}
	if svc == nil {
		svc = NewService()
	}
	every := deps.SweepEvery
	if every == 0 {
		every = sweepEvery
	}
	// Creation is this module's: the JSON create, the web form and the seed all
	// reach the creator, which publishes inside their transaction. A Service that
	// does not create is paired with the module's own implementation.
	creator, ok := svc.(contracts.Creator)
	if !ok {
		own := internal.NewService()
		own.Policy, own.Lifecycle = deps.Policy, svc
		creator = own
	}
	mounted := Spec
	mounted.Create = createWith(creator)
	mounted.Gate = deps.Gate
	return module.Module{
		Name:       "task",
		Migrations: Migrations.Files,
		// The desk's free text is somebody else's note *about* a person, which is
		// why description and resolution are filed as text and not left alone.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name:   "tasks",
			Person: "assignee_id",
			Text:   []string{"title", "description", "resolution"},
		}),
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Roles:       roles,
		Declared:    contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Tasks", Screen: "task/tasks", Permission: contracts.PermissionTaskRead},
		},
		Jobs: []jobs.Job{internal.SLASweep(deps.Tenants, svc, every)},
		// Written out so the absence is a decision: a task is raised by whoever
		// raises it, and this module has no opinion about anybody else's events.
		Subscriptions: nil,
		Routes: func(s httpx.Surfaces) {
			mounted.Mount(s)
			internal.RegisterRoutes(s, mounted, svc)
		},
	}, nil
}

// createWith adapts the creator to the REST seam: the decoded body supplies only
// the fields a CreateTask has, so a body cannot name an id, a revision or a breach.
func createWith(creator contracts.Creator) func(context.Context, db.Tx[db.Tenant], *contracts.Task) (*contracts.Task, error) {
	return func(ctx context.Context, tx db.Tx[db.Tenant], t *contracts.Task) (*contracts.Task, error) {
		return creator.Create(ctx, tx, contracts.CreateTask{
			Title: t.Title, Description: t.Description, Status: t.Status, Priority: t.Priority,
			DueAt: t.DueAt, SLADeadline: t.SLADeadline, Source: t.Source, SourceRef: t.SourceRef,
		})
	}
}
