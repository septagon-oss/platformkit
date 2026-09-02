// Package internal is every implementation of the task module. Nothing outside
// modules/task can import it, which is the compiler enforcing idea 3: a
// consumer takes contracts.Service, and taking anything else does not build.
package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// Service is the task lifecycle. It has no fields: everything a command needs
// arrives with the transaction it is given, which is what lets one instance
// serve a request, a job and an event handler at once, and what makes the whole
// module constructible with no dependency graph at all.
type Service struct{}

// NewService returns the lifecycle commands. It takes nothing, on purpose: see
// the type. module.go constructs it.
func NewService() *Service { return &Service{} }

var _ contracts.Service = (*Service)(nil)

// Assign makes assignee responsible, and acknowledges the task if nobody had
// taken it. The same person twice is the same task and no second event: a
// retried click must not appear in a workload dashboard twice.
func (s *Service) Assign(ctx context.Context, tx db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	if assignee == uuid.Nil {
		return nil, fmt.Errorf("%w: a task is assigned to somebody", crud.ErrInvalid)
	}
	task, err := crud.Get[*contracts.Task](tx, id)
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
	// The three columns this command changed, and no others: a concurrent
	// patch of the description has to survive an assignment, and writing the
	// whole row would put every field back to what this transaction read.
	if err := crud.Update(ctx, tx, task, "assignee_id", "status", "updated_at"); err != nil {
		return nil, err
	}
	return task, events.Publish(ctx, tx, contracts.EventAssigned, contracts.Assigned{
		TaskID: task.ID, Assignee: assignee, Status: task.Status, At: db.Now(),
	})
}

// Resolve closes the loop. Repeating it with the resolution already recorded,
// or with none, changes nothing; a different one on a resolved task is a
// conflict rather than an overwrite, because the account a task gives of itself
// is the auditable part and a retry is not a correction.
func (s *Service) Resolve(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, resolution string) (*contracts.Task, error) {
	resolution = strings.TrimSpace(resolution)
	task, err := crud.Get[*contracts.Task](tx, id)
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
	at := db.Now()
	task.Status = contracts.StatusResolved
	task.Resolution = resolution
	task.ResolvedAt = &at
	if err := crud.Update(ctx, tx, task, "status", "resolution", "resolved_at", "updated_at"); err != nil {
		return nil, err
	}
	return task, events.Publish(ctx, tx, contracts.EventResolved, contracts.Resolved{
		TaskID: task.ID, Resolution: task.Resolution, At: at,
	})
}

// CheckSLA records a breach, once. The sweep calls it every minute for every
// task whose deadline has passed, so "once" is the whole contract: the stored
// flag is read and written in one transaction, and the row lock the update
// takes makes two sweeps racing on one task one breach and one event.
func (s *Service) CheckSLA(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	task, err := crud.Get[*contracts.Task](tx, id)
	if err != nil {
		return nil, err
	}
	if task.SLABreached || !task.IsOverdue(db.Now()) {
		return task, nil
	}
	task.SLABreached = true
	if err := crud.Update(ctx, tx, task, "sla_breached", "updated_at"); err != nil {
		return nil, err
	}
	return task, events.Publish(ctx, tx, contracts.EventSLABreached, contracts.SLABreached{
		TaskID: task.ID, Priority: task.Priority, Deadline: *task.SLADeadline, At: db.Now(),
	})
}
