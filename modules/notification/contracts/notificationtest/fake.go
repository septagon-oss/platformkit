package notificationtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Fake is contracts.Service over a slice: the same rules, no database, no
// transaction. A consumer that wants to test what it does when somebody is
// notified takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, which is the honest limit of it:
// nothing here commits, so it cannot tell a caller that a write did not.
type Fake struct {
	mu    sync.Mutex
	rows  []contracts.Notification
	names []string
	// Addresses is who has an email address, which the real service asks a
	// contracts.RecipientLookup. The fake holds the answer directly rather than
	// taking a lookup, because a fake that needed a collaborator would be a
	// fake somebody has to wire.
	addresses map[uuid.UUID]string
}

// NewFake returns an empty store in which Ada has an address and Bob does not,
// which is what the conformance suite's two cases are about.
func NewFake() *Fake {
	return &Fake{addresses: map[uuid.UUID]string{Ada: AdaEmail}}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted, in order.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.names)
}

// Notify mirrors internal.Service.Notify, validation and all.
func (f *Fake) Notify(ctx context.Context, _ db.Tx[db.Tenant], n contracts.Notice) (*contracts.Notification, error) {
	row := &contracts.Notification{
		RecipientID: n.Recipient, Title: strings.TrimSpace(n.Title),
		Body: n.Body, Link: strings.TrimSpace(n.Link),
	}
	if err := validate(ctx, row); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	row.ID, row.CreatedAt, row.UpdatedAt = uuid.New(), db.Now(), db.Now()
	f.rows = append(f.rows, *row)
	f.names = append(f.names, contracts.EventCreated)
	if n.Email && f.addresses[n.Recipient] != "" {
		f.names = append(f.names, contracts.EventEmailRequested)
	}
	return row, nil
}

// MarkRead mirrors internal.Service.MarkRead.
func (f *Fake) MarkRead(_ context.Context, _ db.Tx[db.Tenant], id, recipient uuid.UUID) (*contracts.Notification, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, row := range f.rows {
		if row.ID != id || row.RecipientID != recipient {
			continue
		}
		if row.ReadAt != nil {
			return &row, nil
		}
		at := db.Now()
		f.rows[i].ReadAt, row.ReadAt = &at, &at
		f.names = append(f.names, contracts.EventRead)
		return &row, nil
	}
	return nil, crud.ErrNotFound
}

// ListFor mirrors internal.Service.ListFor: this recipient's rows, newest
// first, then paged.
func (f *Fake) ListFor(_ context.Context, _ db.Tx[db.Tenant], recipient uuid.UUID, q crud.Query) ([]*contracts.Notification, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var kept []*contracts.Notification
	for i := len(f.rows) - 1; i >= 0; i-- {
		if row := f.rows[i]; row.RecipientID == recipient {
			kept = append(kept, &row)
		}
	}
	total := int64(len(kept))
	kept = kept[min(max(q.Offset, 0), len(kept)):]
	limit := q.Limit
	if limit <= 0 {
		limit = crud.DefaultLimit
	}
	if limit = min(limit, crud.MaxLimit); len(kept) > limit {
		kept = kept[:limit]
	}
	return slices.Clone(kept), total, nil
}

// validate runs the entity's own check and reports it the way kit/crud does, so
// the fake refuses exactly what a write to the database would.
func validate(ctx context.Context, row *contracts.Notification) error {
	if err := row.Validate(ctx); err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	return nil
}
