// Package resolutionsql owns the existing locked Task write and outbox path.
package resolutionsql

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/resolution"
)

type Locked struct {
	tx       db.Tx[db.Tenant]
	actorCtx context.Context
	task     *contracts.Task
}

func Lock(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Locked, error) {
	task, err := crud.GetForUpdate[*contracts.Task](tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil, resolution.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &Locked{tx: tx, actorCtx: ctx, task: task}, nil
}

// Task retains the legacy service's full response without a second row read.
func (l *Locked) Task() *contracts.Task { return l.task }

func (l *Locked) Facts() resolution.Facts {
	t := l.task
	f := resolution.Facts{TenantID: t.TenantID, TaskID: t.ID, Status: t.Status, Priority: t.Priority, Resolution: t.Resolution}
	if t.AssigneeID != nil {
		f.AssigneeID = *t.AssigneeID
	}
	if t.ResolvedAt != nil {
		f.ResolvedAt = new(*t.ResolvedAt)
	}
	return f
}

func (l *Locked) StageResolution(ctx context.Context, change resolution.Change) error {
	t := l.task
	t.Status, t.Resolution, t.ResolvedAt = contracts.StatusResolved, change.Resolution, new(change.At)
	if err := crud.Update(ctx, l.tx, t, "status", "resolution", "resolved_at", "updated_at"); err != nil {
		return err
	}
	return events.Publish(l.actorCtx, l.tx, contracts.EventResolved, contracts.Resolved{
		TaskID: t.ID, Resolution: t.Resolution, At: change.At,
	})
}
