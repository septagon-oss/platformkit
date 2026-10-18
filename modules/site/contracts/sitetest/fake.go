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
	// Palette answers the installation's own accent for a site that chose none,
	// exactly as internal.Service reads it out of Deps: the same value resolves
	// every read and is cleared by every save, so a consumer testing against this
	// fake sees the rules the real service keeps.
	Palette contracts.Palette

	mu        sync.Mutex
	stored    map[uuid.UUID]*contracts.SiteSettings
	published []string

	// home is the partition a context naming no tenant reaches, so that it is a
	// partition of its own rather than every tenant's.
	home uuid.UUID
}

// NewFake returns a site nobody has configured, in an installation that names no
// accent of its own. Set Palette on it to be one that does.
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

// Settings mirrors internal.Service.Settings, resolved colour included.
func (f *Fake) Settings(ctx context.Context, _ db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	key := f.partition(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	out := &contracts.SiteSettings{}
	if stored := f.stored[key]; stored != nil {
		copied := *stored
		out = &copied
	} else if err := out.Validate(ctx); err != nil {
		return out, err
	}
	f.answer(out)
	return out, nil
}

// answer replaces the stored absence of a colour with the colour this installation
// draws with. It is the one rule Fake shares with internal.Service, and the
// conformance suite runs it on both, so neither implementation may hold a version
// of it the other does not.
func (f *Fake) answer(s *contracts.SiteSettings) {
	s.PrimaryColor = s.Resolve(f.Palette)
}

// SettingsForUpdate mirrors internal.Service.SettingsForUpdate. Whether the row is
// locked is a fact about a database, and there is none here: the fake answers the
// same value Settings answers, so a consumer's test sees the same defaults and the
// same revision either way.
func (f *Fake) SettingsForUpdate(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	return f.Settings(ctx, tx)
}

// Save mirrors internal.Service.Save.
func (f *Fake) Save(ctx context.Context, _ db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	key := f.partition(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	// The installation's own colour is the absence of one before anything else
	// reads it, as it is in internal.Service: a value a form echoed is not an edit,
	// and the comparison below has to see what this save will store.
	in.ClearInstallationAccent(f.Palette)
	if err := in.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if f.stored[key] == nil {
		in.ID, in.CreatedAt, in.UpdatedAt = uuid.New(), db.Now(), db.Now()
		in.Revision = 1
		return f.commit(key, in), nil
	}
	in.Base = f.stored[key].Base
	if same(f.stored[key], in) {
		out := *f.stored[key]
		f.answer(&out)
		return &out, nil
	}
	in.Revision = f.stored[key].Revision + 1
	in.UpdatedAt = db.Now()
	return f.commit(key, in), nil
}

// commit stores one tenant's settings and records the event. The caller holds the
// lock. The value handed back is the resolved one, whatever the row keeps.
func (f *Fake) commit(key uuid.UUID, in *contracts.SiteSettings) *contracts.SiteSettings {
	stored := *in
	f.stored[key] = &stored
	f.published = append(f.published, contracts.EventSettingsUpdated)
	out := stored
	f.answer(&out)
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
