// Package savedtest supplies saved's executable contract and a memory adapter.
// It reuses porttest stores, clock, grants and events and saved's pure decisions.
// It proves neither RLS, SQL uniqueness, rollback nor transaction lock ordering.
package savedtest

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
)

// Fake uses the context's tenant instead of its ignored zero transaction. Its
// mutex serializes calls in memory, without representing PostgreSQL's locking.
type Fake struct {
	*porttest.Fake
	mu      sync.Mutex
	items   *porttest.Store[contracts.Item]
	sources []contracts.Savable
}

// NewFake requires an explicit clock and a finite declaration set. Source
// objects are fixture-owned; their data can change to exercise revocation.
func NewFake(at time.Time, sources []contracts.Savable) *Fake {
	return &Fake{Fake: porttest.NewFake(at, []string{contracts.EventSaved, contracts.EventUnsaved}),
		items: porttest.NewStore(func(row contracts.Item) uuid.UUID { return row.ID }), sources: slices.Clone(sources)}
}

// WithoutSources returns an adapter over the same store, clock, authority and
// event recorder with no declaration left in it: the same composition one
// release later, holding copies whose source it no longer names.
func (f *Fake) WithoutSources() *Fake {
	return &Fake{Fake: f.Fake, items: f.items}
}

var _ contracts.Service = (*Fake)(nil)

func (f *Fake) actor(ctx context.Context, permission string) (string, error) {
	tenant, ok := tenancy.FromContext(ctx)
	if !ok || tenant.ID == uuid.Nil {
		return "", db.ErrNoTenant
	}
	principal, signedIn := tenancy.PrincipalFrom(ctx)
	person := principal.UserID
	actor, hasActor := tenancy.ActorFrom(ctx)
	if !signedIn || person == uuid.Nil || (hasActor && actor != person) || !f.Grants.Holds(ctx, permission) {
		return "", contracts.ErrDenied
	}
	return person.String(), nil
}

func (f *Fake) source(ref contracts.Ref) (contracts.Source, error) {
	for _, d := range f.sources {
		if d.Module == ref.Module && d.Kind == ref.Kind && d.Source != nil {
			return d.Source, nil
		}
	}
	return nil, contracts.ErrUnavailable
}

func (f *Fake) own(ctx context.Context, person, raw string) *contracts.Item {
	id, err := internal.ItemID(raw)
	if err != nil {
		return nil
	}
	item, err := f.items.Get(ctx, id)
	if err != nil || item.PersonID != person {
		return nil
	}
	return &item
}

// Save delegates the snapshot/idempotency decision after checking current
// grants and asking the source. The recorded event stands for an outbox insert.
func (f *Fake) Save(ctx context.Context, tx db.Tx[db.Tenant], in contracts.Save) (contracts.Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	person, err := f.actor(ctx, contracts.PermissionWrite)
	if err != nil {
		return contracts.Item{}, err
	}
	if err = internal.ValidSave(in); err != nil {
		return contracts.Item{}, err
	}
	source, err := f.source(in.Source)
	if err != nil {
		return contracts.Item{}, err
	}
	capture, err := source.Capture(ctx, tx, in.Source.Key, in.ExpectedSourceVersion)
	if err != nil {
		return contracts.Item{}, err
	}
	if _, err = f.actor(ctx, contracts.PermissionWrite); err != nil {
		return contracts.Item{}, err
	}
	prior, held := f.items.Find(ctx, func(row contracts.Item) bool { return row.PersonID == person && row.Source == in.Source })
	var current *contracts.Item
	if held {
		current = &prior
	}
	snapshot, changed, err := internal.DecideSave(current, in, capture, f.Clock.Now())
	if err != nil {
		return contracts.Item{}, err
	}
	if !changed {
		return prior, nil
	}
	tenant, _ := tenancy.FromContext(ctx)
	row := contracts.Item{Base: entity.Base{ID: uuid.New(), TenantID: tenant.ID, CreatedAt: snapshot.CapturedAt, UpdatedAt: snapshot.CapturedAt},
		PersonID: person, Source: in.Source, Revision: 1, Snapshot: snapshot}
	if err = f.Events.Emit(contracts.EventSaved); err != nil {
		return contracts.Item{}, err
	}
	f.items.Put(ctx, row)
	return f.items.Get(ctx, row.ID)
}

// Unsave removes only an owned row, checking its revision before removal.
func (f *Fake) Unsave(ctx context.Context, _ db.Tx[db.Tenant], in contracts.Unsave) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	person, err := f.actor(ctx, contracts.PermissionWrite)
	if err != nil {
		return err
	}
	id, err := internal.ItemID(in.ID)
	if err != nil {
		return contracts.ErrInvalid
	}
	row := f.own(ctx, person, in.ID)
	changed, err := internal.DecideUnsave(row, in.ExpectedRevision)
	if err != nil || !changed {
		return err
	}
	if err = f.Events.Emit(contracts.EventUnsaved); err != nil {
		return err
	}
	f.items.Delete(ctx, id)
	return nil
}

func (f *Fake) view(ctx context.Context, tx db.Tx[db.Tenant], item contracts.Item) (contracts.View, error) {
	source, err := f.source(item.Source)
	if err == nil {
		err = source.Check(ctx, tx, item.Source.Key)
	}
	status, err := internal.Availability(err)
	if err != nil {
		return contracts.View{}, err
	}
	return contracts.View{Item: item, Availability: status}, nil
}

func (f *Fake) Get(ctx context.Context, tx db.Tx[db.Tenant], id string) (contracts.View, error) {
	person, err := f.actor(ctx, contracts.PermissionRead)
	if err != nil {
		return contracts.View{}, err
	}
	if _, err = internal.ItemID(id); err != nil {
		return contracts.View{}, err
	}
	row := f.own(ctx, person, id)
	if row == nil {
		return contracts.View{}, contracts.ErrNotFound
	}
	return f.view(ctx, tx, *row)
}

func (f *Fake) Find(ctx context.Context, tx db.Tx[db.Tenant], ref contracts.Ref) (contracts.View, error) {
	person, err := f.actor(ctx, contracts.PermissionRead)
	if err != nil {
		return contracts.View{}, err
	}
	if err = internal.ValidRef(ref); err != nil {
		return contracts.View{}, err
	}
	row, held := f.items.Find(ctx, func(row contracts.Item) bool { return row.PersonID == person && row.Source == ref })
	if !held {
		return contracts.View{}, contracts.ErrNotFound
	}
	return f.view(ctx, tx, row)
}

func (f *Fake) List(ctx context.Context, tx db.Tx[db.Tenant], q contracts.Query) (contracts.Page, error) {
	person, err := f.actor(ctx, contracts.PermissionRead)
	if err != nil {
		return contracts.Page{}, err
	}
	limit, err := internal.PageBounds(q)
	if err != nil {
		return contracts.Page{}, err
	}
	rows := slices.DeleteFunc(f.items.All(ctx), func(row contracts.Item) bool {
		return row.PersonID != person || (q.Module != "" && row.Source.Module != q.Module) || (q.Kind != "" && row.Source.Kind != q.Kind)
	})
	slices.SortFunc(rows, func(a, b contracts.Item) int {
		return cmp.Or(b.Snapshot.CapturedAt.Compare(a.Snapshot.CapturedAt), strings.Compare(b.ID.String(), a.ID.String()))
	})
	out := contracts.Page{Items: []contracts.View{}, Total: int64(len(rows))}
	start := min(q.Offset, len(rows))
	end := start + min(limit, len(rows)-start)
	for _, row := range rows[start:end] {
		view, err := f.view(ctx, tx, row)
		if err != nil {
			return contracts.Page{}, err
		}
		out.Items = append(out.Items, view)
	}
	return out, nil
}

func (f *Fake) Open(ctx context.Context, tx db.Tx[db.Tenant], id string) (string, error) {
	person, err := f.actor(ctx, contracts.PermissionRead)
	if err != nil {
		return "", err
	}
	if _, err = internal.ItemID(id); err != nil {
		return "", err
	}
	row := f.own(ctx, person, id)
	if row == nil {
		return "", contracts.ErrNotFound
	}
	source, err := f.source(row.Source)
	if err != nil {
		return "", err
	}
	path, err := source.Open(ctx, tx, row.Source.Key)
	if err != nil {
		return "", err
	}
	return internal.Destination(path)
}

// Rows returns detached tenant rows for the conformance witness, including rows
// another person owns. It is fixture inspection, never a service read boundary.
func (f *Fake) Rows(ctx context.Context) []contracts.Item { return f.items.All(ctx) }
