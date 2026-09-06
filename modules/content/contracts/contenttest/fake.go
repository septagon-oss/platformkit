package contenttest

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Fake is contracts.Service over a map: the same decisions as the real thing,
// applied to memory instead of to Postgres. A consumer that wants to test what
// it does when a page is published takes one of these instead of a database.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Everything it can be wrong about is what RunService checks.
type Fake struct {
	// Clock is the instant its commands run at, standing in for the
	// transaction's. It is the kernel's clock unless a test pins it.
	Clock func() time.Time

	mu        sync.Mutex
	rows      map[uuid.UUID]contracts.Content
	published []string
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{Clock: db.Now, rows: map[uuid.UUID]contracts.Content{}} }

var _ contracts.Service = (*Fake)(nil)

// Put stores content, giving it an id if it has none, and returns the id. It is
// the fake's stand-in for the create route, normalising the slug the way
// Validate does on the way through.
func (f *Fake) Put(c *contracts.Content) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = db.NewID()
	}
	c.Slug = contracts.Slugify(c.Slug)
	if c.Kind == "" {
		c.Kind = contracts.KindPost
	}
	if c.Status == "" {
		c.Status = contracts.StatusDraft
	}
	f.rows[c.ID] = *c
	return c.ID
}

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Contents is everything the fake holds, for a consumer asserting on state.
func (f *Fake) Contents() map[uuid.UUID]contracts.Content {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.rows)
}

// Publish is contracts.Publish, applied to the map.
func (f *Fake) Publish(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.apply(id, contracts.Publish)
}

// Unpublish is contracts.Unpublish, applied to the map.
func (f *Fake) Unpublish(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.apply(id, func(c contracts.Content, at time.Time) (crud.Outcome[contracts.Content], error) {
		return contracts.Unpublish(c, at), nil
	})
}

// Archive is contracts.Archive, applied to the map.
func (f *Fake) Archive(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.apply(id, func(c contracts.Content, at time.Time) (crud.Outcome[contracts.Content], error) {
		return contracts.Archive(c, at), nil
	})
}

// Public mirrors internal.Service.Public: the published row at this slug and
// nothing else.
func (f *Fake) Public(_ context.Context, _ db.Tx[db.Tenant], slug string) (*contracts.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	slug = contracts.Slugify(slug)
	for _, c := range f.rows {
		if c.Slug == slug && c.Status == contracts.StatusPublished {
			return &c, nil
		}
	}
	return nil, crud.ErrNotFound
}

// apply is crud.Apply over the map: read, decide, and when the decision moved
// the content, store it stamped with the instant and record the event.
func (f *Fake) apply(id uuid.UUID, decide func(contracts.Content, time.Time) (crud.Outcome[contracts.Content], error)) (*contracts.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.rows[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	at := f.Clock()
	out, err := decide(c, at)
	if err != nil {
		return nil, err
	}
	if out.Event == "" {
		return &c, nil
	}
	out.Next.UpdatedAt = at
	f.rows[id] = out.Next
	f.published = append(f.published, out.Event)
	return &out.Next, nil
}
