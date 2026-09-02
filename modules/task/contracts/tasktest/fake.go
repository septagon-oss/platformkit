package tasktest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// Fake is contracts.Service over a map: the same rules, no database, no
// transaction. A consumer that wants to test what it does when a task is
// assigned takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Everything it can be wrong about is what RunService checks, and
// fake_test.go runs the whole suite against it.
type Fake struct {
	mu    sync.Mutex
	tasks map[uuid.UUID]contracts.Task
	// Published is the events the fake would have published, in order, so a
	// consumer can assert on them without an outbox.
	published []string
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{tasks: map[uuid.UUID]contracts.Task{}} }

var _ contracts.Service = (*Fake)(nil)

// Put stores a task, giving it an id if it has none, and returns the id. It is
// the fake's stand-in for the create route.
func (f *Fake) Put(task *contracts.Task) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if task.ID == uuid.Nil {
		task.ID = uuid.New()
	}
	if task.Status == "" {
		task.Status = contracts.StatusOpen
	}
	if task.Priority == "" {
		task.Priority = contracts.PriorityNormal
	}
	f.tasks[task.ID] = *task
	return task.ID
}

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Tasks is every task the fake holds, for a consumer asserting on state rather
// than on a return value.
func (f *Fake) Tasks() map[uuid.UUID]contracts.Task {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.tasks)
}

// Assign mirrors internal.Service.Assign.
func (f *Fake) Assign(_ context.Context, _ db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	if assignee == uuid.Nil {
		return nil, fmt.Errorf("%w: a task is assigned to somebody", crud.ErrInvalid)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	task, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if task.Status == contracts.StatusResolved || task.Status == contracts.StatusClosed {
		return nil, fmt.Errorf("%w: a %s task cannot be assigned", crud.ErrConflict, task.Status)
	}
	if task.AssigneeID != nil && *task.AssigneeID == assignee {
		return task, nil
	}
	task.AssigneeID = &assignee
	if task.Status == contracts.StatusOpen {
		task.Status = contracts.StatusAcknowledged
	}
	return f.commit(task, contracts.EventAssigned), nil
}

// Resolve mirrors internal.Service.Resolve.
func (f *Fake) Resolve(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID, resolution string) (*contracts.Task, error) {
	resolution = strings.TrimSpace(resolution)
	f.mu.Lock()
	defer f.mu.Unlock()
	task, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if task.Status == contracts.StatusClosed {
		return nil, fmt.Errorf("%w: a closed task cannot be resolved", crud.ErrConflict)
	}
	if task.Status == contracts.StatusResolved {
		if resolution == "" || strings.TrimSpace(task.Resolution) == resolution {
			return task, nil
		}
		return nil, fmt.Errorf("%w: this task is resolved with a different resolution", crud.ErrConflict)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	task.Status, task.Resolution, task.ResolvedAt = contracts.StatusResolved, resolution, &at
	return f.commit(task, contracts.EventResolved), nil
}

// CheckSLA mirrors internal.Service.CheckSLA.
func (f *Fake) CheckSLA(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if task.SLABreached || !task.IsOverdue(time.Now()) {
		return task, nil
	}
	task.SLABreached = true
	return f.commit(task, contracts.EventSLABreached), nil
}

// get is a copy of the stored task, so a caller that mutates what it was
// handed does not reach into the store — which is what a database would do.
func (f *Fake) get(id uuid.UUID) (*contracts.Task, error) {
	stored, ok := f.tasks[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	return &stored, nil
}

// commit stores the changed task and records the event. The caller holds the
// lock.
func (f *Fake) commit(task *contracts.Task, event string) *contracts.Task {
	task.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	f.tasks[task.ID] = *task
	f.published = append(f.published, event)
	return task
}
