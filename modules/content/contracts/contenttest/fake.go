package contenttest

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Fake is contracts.Service over a map: the same rules, no database, no
// transaction. A consumer that wants to test what it does when a page is
// published takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Everything it can be wrong about is what RunService checks.
type Fake struct {
	mu        sync.Mutex
	rows      map[uuid.UUID]contracts.Content
	published []string
}

// NewFake returns an empty store.
func NewFake() *Fake { return &Fake{rows: map[uuid.UUID]contracts.Content{}} }

var _ contracts.Service = (*Fake)(nil)

// Put stores content, giving it an id if it has none, and returns the id. It is
// the fake's stand-in for the create route, normalising the slug the way
// Validate does on the way through.
func (f *Fake) Put(c *contracts.Content) uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
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

// Publish mirrors internal.Service.Publish.
func (f *Fake) Publish(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return nil, err
	}
	switch c.Status {
	case contracts.StatusPublished:
		return c, nil
	case contracts.StatusArchived:
		return nil, fmt.Errorf("%w: archived content is not published from the archive; unpublish it first", crud.ErrConflict)
	}
	at := db.Now()
	c.Status, c.PublishedAt = contracts.StatusPublished, &at
	return f.commit(c, contracts.EventPublished), nil
}

// Unpublish mirrors internal.Service.Unpublish.
func (f *Fake) Unpublish(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.to(id, contracts.StatusDraft, contracts.EventUnpublished)
}

// Archive mirrors internal.Service.Archive.
func (f *Fake) Archive(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.to(id, contracts.StatusArchived, contracts.EventArchived)
}

func (f *Fake) to(id uuid.UUID, status, event string) (*contracts.Content, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return nil, err
	}
	if c.Status == status {
		return c, nil
	}
	c.Status, c.PublishedAt = status, nil
	return f.commit(c, event), nil
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

// get is a copy of the stored content, so a caller that mutates what it was
// handed does not reach into the store. The caller holds the lock.
func (f *Fake) get(id uuid.UUID) (*contracts.Content, error) {
	stored, ok := f.rows[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	return &stored, nil
}

// commit stores the changed content and records the event. The caller holds
// the lock.
func (f *Fake) commit(c *contracts.Content, event string) *contracts.Content {
	c.UpdatedAt = db.Now()
	f.rows[c.ID] = *c
	f.published = append(f.published, event)
	return c
}
