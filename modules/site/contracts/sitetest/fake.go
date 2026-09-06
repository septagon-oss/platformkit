package sitetest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Fake is contracts.Service over one value: the same decision as the real
// thing, applied to memory instead of to Postgres. A consumer that wants to
// test what it does when a site is reconfigured takes one of these instead of a
// database.
//
// It stands in for one tenant, which is the scope a tenant transaction has, and
// it ignores the transaction it is handed — the honest limit of it being that
// nothing here commits.
type Fake struct {
	// Clock is the instant its commands run at, standing in for the
	// transaction's. It is the kernel's clock unless a test pins it.
	Clock func() time.Time

	mu        sync.Mutex
	stored    *contracts.SiteSettings
	published []string
}

// NewFake returns a site nobody has configured.
func NewFake() *Fake { return &Fake{Clock: db.Now} }

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

// Save is contracts.Save, applied to the one value: a first save is created
// and a change is stored, both stamped with the instant, and the event recorded.
func (f *Fake) Save(_ context.Context, _ db.Tx[db.Tenant], in *contracts.SiteSettings) (*contracts.SiteSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := f.Clock()
	out, err := contracts.Save(f.stored, *in, at, db.NewID())
	if err != nil {
		return nil, err
	}
	if out.Event == "" {
		return &out.Next, nil
	}
	if f.stored == nil {
		out.Next.CreatedAt = at
	}
	out.Next.UpdatedAt = at
	stored := out.Next
	f.stored = &stored
	f.published = append(f.published, out.Event)
	return &out.Next, nil
}
