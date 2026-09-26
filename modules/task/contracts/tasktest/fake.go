package tasktest

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/domain"
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
// wrong about is what RunService checks, and fake_test.go runs the whole suite
// against it.
//
// Its store is partitioned by the tenant on the context and refuses a context
// that names none, so a consumer cannot test a command outside a tenant by
// accident. The three commands go through porttest.Do, which holds every command
// to one order — load, decide, and only then write and say so — so a refusal
// here cannot leave a row written or an event published.
type Fake struct {
	*porttest.Fake
	tasks *porttest.Store[contracts.Task]
}

// NewFake returns an empty store, its clock reading now.
func NewFake() *Fake {
	return &Fake{
		Fake: porttest.NewFake(db.Now(), []string{
			contracts.EventAssigned, contracts.EventResolved, contracts.EventSLABreached,
		}),
		tasks: porttest.NewStore(func(task contracts.Task) uuid.UUID { return task.ID }),
	}
}

var _ contracts.Service = (*Fake)(nil)

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
	task.ID = porttest.Seed(ctx, f.Fake, f.tasks, *task)
	return task.ID
}

// Task is one task as the fake holds it, by value, so a caller that writes to
// what it was handed does not reach into the store. The conformance suite reads
// it to say what "a refused command wrote nothing" means.
func (f *Fake) Task(ctx context.Context, id uuid.UUID) (contracts.Task, error) {
	return f.tasks.Get(ctx, id)
}

// Assign mirrors internal.Service.Assign.
func (f *Fake) Assign(ctx context.Context, _ db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	if assignee == uuid.Nil {
		return nil, fmt.Errorf("%w: a task is assigned to somebody", crud.ErrInvalid)
	}
	return f.command(ctx, porttest.Command[contracts.Task]{
		Row: id,
		Decide: func(task contracts.Task) error {
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
			task.UpdatedAt = at
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
			task.SLABreached, task.UpdatedAt = true, f.Clock.Now()
			return []string{contracts.EventSLABreached}
		},
	})
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
