package contenttest

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// Fake is contracts.Service over kit/porttest's plumbing: the same rules, no
// database, no transaction. A consumer that wants to test what it does when a
// page is published takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. Nor can it claim row-level security, the unique index on a slug or
// the row lock that settles two writers — those are database facts, and
// modules/content/internal proves them against the schema.
//
// What it does hold is one tenant's rows, and only one. The first tenant to
// reach it through the port owns them; every other tenant reads a store with
// nothing in it and writes nothing, which is the answer row-level security
// gives. A fixture that acts through a context naming no tenant is that tenant
// acting outside a request, so a consumer can seed and read without building
// one. A fake that shared one map across tenants would pass a cross-tenant read
// the database refuses, which is the opposite of what a fake is for.
//
// The three commands go through porttest.Do, which holds every command to one
// order — load, decide, and only then write and say so — so a refusal here
// cannot leave a row written or an event published.
type Fake struct {
	*porttest.Fake
	rows *porttest.Store[contracts.Content]

	mu sync.Mutex
	// home is the partition the rows are kept in, and owner the tenant that
	// holds them: unset until the first call that names one.
	home  tenancy.Tenant
	owner uuid.UUID
}

// NewFake returns an empty store, its clock reading now.
func NewFake() *Fake {
	return &Fake{
		Fake: porttest.NewFake(db.Now(), []string{
			contracts.EventPublished, contracts.EventUnpublished, contracts.EventArchived,
		}),
		rows: porttest.NewStore(func(c contracts.Content) uuid.UUID { return c.ID }),
		home: tenancy.Tenant{ID: uuid.New(), Slug: "contenttest"},
	}
}

var _ contracts.Service = (*Fake)(nil)

// scope is the partition a call reaches the store through: this fake's own for
// the tenant that owns the rows and for a fixture that names none, and the
// caller's own — which is empty, and stays empty — for anybody else.
func (f *Fake) scope(ctx context.Context) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, named := tenancy.FromContext(ctx)
	if !named {
		return tenancy.WithTenant(ctx, f.home)
	}
	if f.owner == uuid.Nil {
		f.owner = t.ID
	}
	if t.ID != f.owner {
		return ctx
	}
	return tenancy.WithTenant(ctx, f.home)
}

// Put stores content, giving it an id if it has none, and returns the id. It is
// the fake's stand-in for the create route: the entity's own Validate
// normalises the slug and fills the defaults in on the way through, and refuses
// a row the database would refuse.
func (f *Fake) Put(c *contracts.Content) uuid.UUID {
	ctx := f.scope(context.Background())
	id := porttest.Seed(ctx, f.Fake, f.rows, *c)
	stored, err := f.rows.Get(ctx, id)
	if err != nil {
		panic(fmt.Sprintf("contenttest: seeded content is not in the store: %v", err))
	}
	*c = stored
	return id
}

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string { return f.Events.Names() }

// Contents is everything the fake holds, for a consumer asserting on state.
func (f *Fake) Contents() map[uuid.UUID]contracts.Content {
	out := map[uuid.UUID]contracts.Content{}
	for _, c := range f.rows.All(f.scope(context.Background())) {
		out[c.ID] = c
	}
	return out
}

// Content is one row as the fake holds it, which is what the suite's snapshot
// of "a refused command wrote nothing" compares either side of a call.
func (f *Fake) Content(id uuid.UUID) (contracts.Content, error) {
	return f.rows.Get(f.scope(context.Background()), id)
}

// Publish mirrors internal.Service.Publish.
func (f *Fake) Publish(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.command(ctx, porttest.Command[contracts.Content]{
		Row: id,
		Decide: func(c contracts.Content) error {
			if c.Status == contracts.StatusArchived {
				return fmt.Errorf("%w: archived content is not published from the archive; unpublish it first", crud.ErrConflict)
			}
			return nil
		},
		Apply: func(c *contracts.Content) []string {
			if c.Status == contracts.StatusPublished {
				return nil // a second click on the button, not a second publication
			}
			at := f.Clock.Now()
			c.Status, c.PublishedAt, c.UpdatedAt = contracts.StatusPublished, &at, at
			return []string{contracts.EventPublished}
		},
	})
}

// Unpublish mirrors internal.Service.Unpublish.
func (f *Fake) Unpublish(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.to(ctx, id, contracts.StatusDraft, contracts.EventUnpublished)
}

// Archive mirrors internal.Service.Archive.
func (f *Fake) Archive(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.Content, error) {
	return f.to(ctx, id, contracts.StatusArchived, contracts.EventArchived)
}

func (f *Fake) to(ctx context.Context, id uuid.UUID, status, event string) (*contracts.Content, error) {
	return f.command(ctx, porttest.Command[contracts.Content]{
		Row: id,
		Apply: func(c *contracts.Content) []string {
			if c.Status == status {
				return nil // already there: nothing to write and nothing to say
			}
			c.Status, c.PublishedAt, c.UpdatedAt = status, nil, f.Clock.Now()
			return []string{event}
		},
	})
}

// Public mirrors internal.Service.Public: the published row at this slug, of
// this tenant, and nothing else.
func (f *Fake) Public(ctx context.Context, _ db.Tx[db.Tenant], slug string) (*contracts.Content, error) {
	slug = contracts.Slugify(slug)
	got, found := f.rows.Find(f.scope(ctx), func(c contracts.Content) bool {
		return c.Slug == slug && c.Status == contracts.StatusPublished
	})
	if !found {
		return nil, crud.ErrNotFound
	}
	return &got, nil
}

// command runs one of the three and answers with the content as it now stands.
// The port returns a pointer, and this is where the copy the store handed back
// becomes one: a caller cannot reach the row itself.
func (f *Fake) command(ctx context.Context, c porttest.Command[contracts.Content]) (*contracts.Content, error) {
	got, err := porttest.Do(f.scope(ctx), f.Fake, f.rows, c)
	if err != nil {
		return nil, err
	}
	return &got, nil
}
