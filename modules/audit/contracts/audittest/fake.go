package audittest

import (
	"context"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
)

// Fake is contracts.Service over a slice: the same rules, no database, no
// transaction. A consumer that wants to test what it does with a trail takes
// one of these instead of a Postgres.
//
// It ignores the transaction it is handed, which is the honest limit of it:
// nothing here commits, so it cannot tell a caller that a write did not. It
// keeps rows in the order they arrived and reverses them on the way out,
// because "newest first" is what the interface promises and a consumer that
// tested against arrival order would be testing the wrong thing.
type Fake struct {
	mu   sync.Mutex
	rows []contracts.Event
}

// NewFake returns an empty trail.
func NewFake() *Fake { return &Fake{} }

var _ contracts.Service = (*Fake)(nil)

// Record mirrors internal.Service.Record, idempotency included.
func (f *Fake) Record(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.EventID == ev.ID {
			return nil
		}
	}
	row := contracts.Event{
		ID: uuid.New(), OccurredAt: ev.At, Name: ev.Name,
		EventID: ev.ID, Payload: ev.Payload,
	}
	if ev.Actor != uuid.Nil {
		actor := ev.Actor
		row.Actor = &actor
	}
	f.rows = append(f.rows, row)
	return nil
}

// List mirrors internal.Service.List: newest first, filtered, then paged.
func (f *Fake) List(_ context.Context, _ db.Tx[db.Tenant], q contracts.Query) ([]*contracts.Event, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []*contracts.Event
	for i := len(f.rows) - 1; i >= 0; i-- {
		row := f.rows[i]
		switch {
		case q.Name != "" && row.Name != q.Name:
		case q.Actor != uuid.Nil && (row.Actor == nil || *row.Actor != q.Actor):
		case !q.Since.IsZero() && row.OccurredAt.Before(q.Since):
		case !q.Until.IsZero() && !row.OccurredAt.Before(q.Until):
		default:
			kept = append(kept, &row)
		}
	}
	total := int64(len(kept))
	kept = kept[min(max(q.Offset, 0), len(kept)):]
	if limit := page(q.Limit); len(kept) > limit {
		kept = kept[:limit]
	}
	return slices.Clone(kept), total, nil
}

// Get mirrors internal.Service.Get.
func (f *Fake) Get(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.rows {
		if row.ID == id {
			return &row, nil
		}
	}
	return nil, crud.ErrNotFound
}

// page is kit/crud's bounds, so the fake and the real service agree about what
// a caller who asked for nothing gets.
func page(limit int) int {
	if limit <= 0 {
		return crud.DefaultLimit
	}
	return min(limit, crud.MaxLimit)
}
