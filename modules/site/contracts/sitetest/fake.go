package sitetest

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Fake is contracts.Service over one value: the same rules, no database, no
// transaction. A consumer that wants to test what it does when a site is
// reconfigured takes one of these instead of a Postgres.
//
// It stands in for one tenant, which is the scope a tenant transaction has, and
// it ignores the transaction it is handed — the honest limit of it being that
// nothing here commits.
type Fake struct {
	mu        sync.Mutex
	stored    *contracts.SiteSettings
	published []string
}

// NewFake returns a site nobody has configured.
func NewFake() *Fake { return &Fake{} }

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Settings mirrors internal.Service.Settings.
func (f *Fake) Settings(ctx context.Context, _ db.Tx[db.Tenant]) (*contracts.SiteSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stored == nil {
		out := &contracts.SiteSettings{}
		return out, out.Validate(ctx)
	}
	out := *f.stored
	return &out, nil
}

// Save mirrors internal.Service.Save.
func (f *Fake) Save(ctx context.Context, _ db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := in.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if f.stored == nil {
		in.ID, in.CreatedAt, in.UpdatedAt = uuid.New(), db.Now(), db.Now()
		return f.commit(in), nil
	}
	in.Base = f.stored.Base
	if same(f.stored, in) {
		out := *f.stored
		return &out, nil
	}
	in.UpdatedAt = db.Now()
	return f.commit(in), nil
}

// commit stores the settings and records the event. The caller holds the lock.
func (f *Fake) commit(in *contracts.SiteSettings) *contracts.SiteSettings {
	stored := *in
	f.stored = &stored
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
