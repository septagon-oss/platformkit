package tasktest

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/domain"
	// The one non-test file under modules/*/contracts/ that reaches into its own
	// module's internal package: legal because that rule is rooted at the parent of
	// internal/ and this package sits under modules/task/, and sound because the code
	// it calls is this module's creation decision and nothing else's.
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// Fake is contracts.Service over kit/porttest's plumbing: the same rules, no
// database, no transaction. A consumer that wants to test what it does when a
// task is assigned takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Nor can it claim row-level security, the unique indexes or the row
// lock that settles two writers — those are database facts, and
// modules/task/internal proves them against the schema. Everything it can be
// wrong about is what RunService checks, and the fake's own tests run the whole suite
// against it.
//
// Its store is partitioned by the tenant on the context and refuses a context
// that names none, so a consumer cannot test a command outside a tenant by
// accident. Every write it accepts moves the row's own Revision, as
// internal.Service does, so a consumer that reads that number off a fake row
// sees what the real service would have answered; no command of this port takes
// a number back from a caller, which is what the suite's declined Stale case
// states and its guard test watches. The three commands go through porttest.Do, which holds every command
// to one order — load, decide, and only then write and say so — so a refusal
// here cannot leave a row written or an event published.
type Fake struct {
	*porttest.Fake
	tasks *porttest.Store[contracts.Task]
	// Policy is the same tenancy.Policy internal.Service.Policy holds, asked the
	// same question before either command writes. Left nil it retains the
	// composition's declared grants, exactly as the real service does, so a
	// consumer that never tests a refusal sees the fake it had.
	Policy tenancy.Policy
}

// NewFake returns an empty store, its clock reading now.
func NewFake() *Fake {
	return &Fake{
		Fake: porttest.NewFake(db.Now(), []string{
			contracts.EventCreated, contracts.EventAssigned, contracts.EventResolved, contracts.EventSLABreached,
		}),
		tasks: porttest.NewStore(func(task contracts.Task) uuid.UUID { return task.ID }),
	}
}

var (
	_ contracts.Service      = (*Fake)(nil)
	_ contracts.Creator      = (*Fake)(nil)
	_ contracts.Reader       = (*Fake)(nil)
	_ contracts.LockedReader = (*Fake)(nil)
)

// TaskForUpdate is contracts.LockedReader over the fake's store: the same row the
// plain read returns.
//
// It fakes the read and nothing else. The row lock is a Postgres fact, and the
// reason this fake hands back no transaction is the reason it cannot hold one;
// modules/task/internal proves the lock against the schema. What this method is for
// is the consumer's own apply path — a change subject tested end to end against
// this double exercises the read its diff was made against, and only the real
// service's suite can show that read was locked while it happened.
func (f *Fake) TaskForUpdate(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	return f.TaskByID(ctx, db.Tx[db.Tenant]{}, id)
}

// Put stores a task for the tenant on the context, giving it an id if it has
// none, and returns the id. It is the fake's stand-in for the create route, and
// it refuses a task the entity's own Validate refuses.
func (f *Fake) Put(ctx context.Context, task *contracts.Task) uuid.UUID {
	if task.Status == "" {
		task.Status = contracts.StatusOpen
	}
	if task.Priority == "" {
		task.Priority = contracts.PriorityNormal
	}
	if task.Revision == 0 {
		task.Revision = 1 // a created row reads 1: there, gorm's default, and here, the line below
	}
	task.ID = porttest.Seed(ctx, f.Fake, f.tasks, *task)
	return task.ID
}

// Task is one task as the fake holds it, by value, so a caller that writes to
// what it was handed does not reach into the store. The conformance suite reads
// it to say what "a refused command wrote nothing" means.
func (f *Fake) Task(ctx context.Context, id uuid.UUID) (contracts.Task, error) {
	return f.tasks.Get(ctx, id)
}

// Create mirrors the Creator the module's service will implement. Every refusal
// is decided before the store is touched, so a refused create writes nothing and
// says nothing — and the refusal is the service's own decision, made by the same
// call internal.Service.Create makes, so the two worlds cannot disagree about an
// input. Like the commands it ignores the transaction, which is why the rollback
// cases of CreatorSuite are the real service's alone.
func (f *Fake) Create(ctx context.Context, _ db.Tx[db.Tenant], in contracts.CreateTask) (*contracts.Task, error) {
	task, err := internal.NewTask(ctx, in)
	if err != nil {
		return nil, err
	}
	task.Revision = 1 // a created row reads 1: there, gorm's default, and here, this line
	says := []string{contracts.EventCreated}
	if in.AssigneeID != nil {
		task.AssigneeID = in.AssigneeID
		if task.Status == contracts.StatusOpen {
			task.Status = contracts.StatusAcknowledged
		}
		says = append(says, contracts.EventAssigned)
	}
	if task.IsOverdue(f.Clock.Now()) {
		task.SLABreached, task.Revision = true, task.Revision+1
		says = append(says, contracts.EventSLABreached)
	}
	task.ID = porttest.Seed(ctx, f.Fake, f.tasks, *task)
	for _, name := range says {
		if err := f.Events.Emit(name); err != nil {
			return nil, err
		}
	}
	stored, err := f.tasks.Get(ctx, task.ID)
	return &stored, err
}

// Count is how many tasks the fake holds for the tenant on the context.
func (f *Fake) Count(ctx context.Context) int { return len(f.tasks.All(ctx)) }

// TaskByID is contracts.Reader over the fake's store.
func (f *Fake) TaskByID(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	task, err := f.tasks.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// Assign mirrors internal.Service.Assign.
func (f *Fake) Assign(ctx context.Context, _ db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	if assignee == uuid.Nil {
		return nil, fmt.Errorf("%w: a task is assigned to somebody", crud.ErrInvalid)
	}
	return f.command(ctx, porttest.Command[contracts.Task]{
		Row: id,
		Decide: func(task contracts.Task) error {
			if err := f.authorize(ctx, task, "task:assign", assignee); err != nil {
				return err
			}
			if task.Status == contracts.StatusResolved || task.Status == contracts.StatusClosed {
				return fmt.Errorf("%w: a %s task cannot be assigned", crud.ErrConflict, task.Status)
			}
			return nil
		},
		Apply: func(task *contracts.Task) []string {
			if task.AssigneeID != nil && *task.AssigneeID == assignee {
				return nil // the same person again: nothing to write and nothing to say
			}
			task.AssigneeID = &assignee
			if task.Status == contracts.StatusOpen {
				task.Status = contracts.StatusAcknowledged
			}
			task.UpdatedAt = f.Clock.Now()
			task.Revision++
			return []string{contracts.EventAssigned}
		},
	})
}

// Resolve shares the production resolution decision, without persistence.
func (f *Fake) Resolve(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID, resolution string) (*contracts.Task, error) {
	// The loop's own decision, made over the task as stored and applied only if
	// it was accepted: a resolution the domain refuses never reaches the row.
	var decision domain.Resolution
	return f.command(ctx, porttest.Command[contracts.Task]{
		Row: id,
		Decide: func(task contracts.Task) error {
			if err := f.authorize(ctx, task, "task:resolve", uuid.Nil); err != nil {
				return err
			}
			proposed, err := domain.Resolve(task.Status, task.Resolution, resolution)
			if err != nil {
				return fmt.Errorf("%w: %v", crud.ErrConflict, err)
			}
			decision = proposed
			return nil
		},
		Apply: func(task *contracts.Task) []string {
			if !decision.Changed {
				return nil
			}
			at := f.Clock.Now()
			task.Status, task.Resolution, task.ResolvedAt = contracts.StatusResolved, decision.Text, &at
			task.UpdatedAt, task.Revision = at, task.Revision+1
			return []string{contracts.EventResolved}
		},
	})
}

// CheckSLA mirrors internal.Service.CheckSLA.
func (f *Fake) CheckSLA(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	return f.command(ctx, porttest.Command[contracts.Task]{
		Row: id,
		Apply: func(task *contracts.Task) []string {
			if task.SLABreached || !task.IsOverdue(f.Clock.Now()) {
				return nil // the sweep runs every minute forever, so this is the ordinary case
			}
			task.SLABreached, task.UpdatedAt, task.Revision = true, f.Clock.Now(), task.Revision+1
			return []string{contracts.EventSLABreached}
		},
	})
}

// authorize sends the same locked-row facts as the SQL service. Stranger remains
// the fixture's caller without a grant, independent of the configured policy.
func (f *Fake) authorize(ctx context.Context, task contracts.Task, action string, assignee uuid.UUID) error {
	if porttest.Actor(ctx) == Stranger {
		return tenancy.ErrPolicyDenied
	}
	if f.Policy == nil {
		return nil // Compositions without an external policy retain their declared grants.
	}
	principal, ok := tenancy.PrincipalFrom(ctx)
	if !ok || principal.UserID == uuid.Nil {
		return tenancy.ErrPolicyDenied
	}
	tenant, _ := tenancy.FromContext(ctx) // porttest.Do refused a context naming no tenant already
	attributes := map[string]any{"status": task.Status, "priority": task.Priority, "assignee_id": ""}
	if task.AssigneeID != nil {
		attributes["assignee_id"] = task.AssigneeID.String()
	}
	if assignee != uuid.Nil {
		attributes["requested_assignee_id"] = assignee.String()
	}
	_, err := tenancy.RequirePolicy(ctx, f.Policy, tenancy.PolicyRequest{
		Tenant: tenant,
		Actor:  tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: principal.UserID.String()},
		Action: action,
		Resource: tenancy.PolicyResource{
			TenantID: task.TenantID, Kind: "task", ID: task.ID.String(), Attributes: attributes,
		},
	})
	return err
}

// command runs one of the three and answers with the task as it now stands. The
// port returns a pointer, and this is where the copy the store handed back
// becomes one: a caller cannot reach the row itself.
func (f *Fake) command(ctx context.Context, c porttest.Command[contracts.Task]) (*contracts.Task, error) {
	task, err := porttest.Do(ctx, f.Fake, f.tasks, c)
	if err != nil {
		return nil, err
	}
	return &task, nil
}
