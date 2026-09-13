// Package internal is every implementation of the task module. Nothing outside
// modules/task can import it, which is the compiler enforcing idea 3: a
// consumer takes contracts.Service, and taking anything else does not build.
package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/domain"
)

// Service owns task transitions. Optional policy decisions use the locked task
// and the current principal; they never replace the lifecycle's state checks.
type Service struct{ Policy tenancy.Policy }

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
	task, err := crud.GetForUpdate[*contracts.Task](tx, id)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, tx, task, "task:assign", assignee); err != nil {
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
	task, err := crud.GetForUpdate[*contracts.Task](tx, id)
	if err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, tx, task, "task:resolve", uuid.Nil); err != nil {
		return nil, err
	}
	decision, err := domain.Resolve(task.Status, task.Resolution, resolution)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrConflict, err)
	}
	if !decision.Changed {
		return task, nil
	}
	at := db.Now()
	task.Status = contracts.StatusResolved
	task.Resolution = decision.Text
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
// row is locked before reading the flag, deadline and resolution, so a waiting
// command evaluates the preceding writer's committed state before deciding.
func (s *Service) CheckSLA(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	task, err := crud.GetForUpdate[*contracts.Task](tx, id)
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
