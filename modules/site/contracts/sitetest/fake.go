package sitetest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Fake is contracts.Service over one value per tenant: the same rules, no
// database, no transaction. A consumer that wants to test what it does when a
// site is reconfigured takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// nothing here commits. What it does hold is one settings row per tenant, the
// way row-level security gives the real service one per tenant: two tenants
// driving one fake never read or overwrite each other's settings. A call whose
// context names no tenant reaches the fake's own partition, which is how a
// fixture seeds and reads without building a request; a second tenant never
// reaches it. The event log is the one thing it does not partition — Published
// takes no context, so a consumer driving two tenants over one fake reads both
// tenants' event names there, and modules/site/internal proves the outbox rows
// are the tenant's own against the schema.
type Fake struct {
	mu        sync.Mutex
	stored    map[uuid.UUID]*contracts.SiteSettings
	published []string

	// home is the partition a context naming no tenant reaches, so that it is a
	// partition of its own rather than every tenant's.
	home uuid.UUID
}

// NewFake returns a site nobody has configured.
func NewFake() *Fake {
	return &Fake{stored: map[uuid.UUID]*contracts.SiteSettings{}, home: uuid.New()}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// partition is the key a call reaches the stored settings through: the tenant on
// the context, or the fake's own home when the context names none. It reads only
// the immutable home, so it runs outside the lock the store needs.
func (f *Fake) partition(ctx context.Context) uuid.UUID {
	tenant, named := tenancy.FromContext(ctx)
	if !named {
		return f.home
	}
	return tenant.ID
}

// Settings mirrors internal.Service.Settings.
func (f *Fake) Settings(ctx context.Context, _ db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	key := f.partition(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stored[key] == nil {
		out := &contracts.SiteSettings{}
		return out, out.Validate(ctx)
	}
	out := *f.stored[key]
	return &out, nil
}

// Save mirrors internal.Service.Save.
func (f *Fake) Save(ctx context.Context, _ db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	key := f.partition(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := in.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if f.stored[key] == nil {
		in.ID, in.CreatedAt, in.UpdatedAt = uuid.New(), db.Now(), db.Now()
		return f.commit(key, in), nil
	}
	in.Base = f.stored[key].Base
	if same(f.stored[key], in) {
		out := *f.stored[key]
		return &out, nil
	}
	in.UpdatedAt = db.Now()
	return f.commit(key, in), nil
}

// commit stores one tenant's settings and records the event. The caller holds the
// lock.
func (f *Fake) commit(key uuid.UUID, in *contracts.SiteSettings) *contracts.SiteSettings {
	stored := *in
	f.stored[key] = &stored
	f.published = append(f.published, contracts.EventSettingsUpdated)
	out := stored
	return &out
}

// same is internal.same: whether saving in would change anything a reader could
// see.
func same(a, b *contracts.SiteSettings) bool {
	if a.Title != b.Title || a.Tagline != b.Tagline || a.HomeSlug != b.HomeSlug ||
		a.Theme != b.Theme || a.PrimaryColor != b.PrimaryColor {
		return false
	}
	if (a.LogoFileID == nil) != (b.LogoFileID == nil) ||
		(a.LogoFileID != nil && *a.LogoFileID != *b.LogoFileID) {
		return false
	}
	return slices.Equal(a.Nav, b.Nav)
}
