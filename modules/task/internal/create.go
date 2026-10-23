package internal

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

var (
	_ contracts.Creator = (*Service)(nil)
	_ contracts.Reader  = (*Service)(nil)
)

// NewReader returns the plain read of a task by id.
func NewReader() contracts.Reader { return NewService() }

// Create is contracts.Creator: it checks the input, asks the policy, inserts the
// row, publishes created (then assigned, then sla_breached when they apply) and
// returns the row as it now stands. Every refusal is decided before the insert.
func (s *Service) Create(ctx context.Context, tx db.Tx[db.Tenant], in contracts.CreateTask) (*contracts.Task, error) {
	task, err := newTask(ctx, in)
	if err != nil {
		return nil, err
	}
	task.ID = uuid.New()
	if err := crud.Create(ctx, tx, task); err != nil {
		return nil, err
	}
	if err := events.Succeed(ctx, tx, db.Now()); err != nil {
		return nil, err
	}
	if err := events.Publish(ctx, tx, contracts.EventCreated, task); err != nil {
		return nil, err
	}
	if in.AssigneeID != nil {
		if task, err = s.lifecycle().Assign(ctx, tx, task.ID, *in.AssigneeID); err != nil {
			return nil, err
		}
	}
	return s.lifecycle().CheckSLA(ctx, tx, task.ID)
}

// newTask is the entity a CreateTask describes, normalised and checked.
func newTask(ctx context.Context, in contracts.CreateTask) (*contracts.Task, error) {
	if err := refuseInput(in); err != nil {
		return nil, err
	}
	task := &contracts.Task{
		Title: in.Title, Description: in.Description, Status: in.Status, Priority: in.Priority,
		DueAt: utc(in.DueAt), SLADeadline: utc(in.SLADeadline), Source: in.Source, SourceRef: in.SourceRef,
	}
	if err := task.Validate(ctx); err != nil {
		// Both halves are wrapped, the way kit/crud wraps the same check: errors.Is
		// still answers ErrInvalid for the JSON door, and the refusal the entity
		// wrote stays reachable as the value it is, so a redrawn form can read the
		// key off it and answer the refusal in the reader's language. The reason as
		// text would leave the English sentence on a page written in another one.
		return nil, fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	return task, nil
}

// refuseInput is the check Task.Validate cannot make: the column widths of
// source and sourceRef, an assignee that is nobody, and a time that is no instant.
func refuseInput(in contracts.CreateTask) error {
	switch {
	case utf8.RuneCountInString(in.Source) > 40:
		return fmt.Errorf("%w: source must have at most 40 characters", crud.ErrInvalid)
	case utf8.RuneCountInString(in.SourceRef) > 120:
		return fmt.Errorf("%w: sourceRef must have at most 120 characters", crud.ErrInvalid)
	case in.AssigneeID != nil && *in.AssigneeID == uuid.Nil:
		return fmt.Errorf("%w: a task is assigned to somebody", crud.ErrInvalid)
	case in.DueAt != nil && in.DueAt.IsZero(), in.SLADeadline != nil && in.SLADeadline.IsZero():
		return fmt.Errorf("%w: a time that is unsaid is nil, not the zero instant", crud.ErrInvalid)
	}
	return nil
}

// TaskByID is contracts.Reader.
func (s *Service) TaskByID(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	return crud.Get[*contracts.Task](tx, id)
}

func utc(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	u := at.UTC()
	return &u
}

// lifecycle is the service Create's follow-up commands go through: the composed
// one when the module was given one, otherwise the receiver itself.
func (s *Service) lifecycle() contracts.Service {
	if s.Lifecycle != nil {
		return s.Lifecycle
	}
	return s
}
