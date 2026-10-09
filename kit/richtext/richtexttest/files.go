// Package richtexttest supplies a tenant-scoped Files fake for richtext consumers.
package richtexttest

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type key struct{ tenant, file uuid.UUID }
type entry struct {
	image  richtext.Image
	public bool
}

// FakeFiles uses the tenant carried by db.Tx, not a context or process setting.
type FakeFiles struct {
	mu     sync.RWMutex
	images map[key]entry
	Err    error
}

// Put makes an image visible to its own tenant; public controls anonymous reads.
func (f *FakeFiles) Put(tenant tenancy.Tenant, id uuid.UUID, image richtext.Image, public bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.images == nil {
		f.images = make(map[key]entry)
	}
	f.images[key{tenant.ID, id}] = entry{image, public}
}

// Remove forgets one tenant's image.
func (f *FakeFiles) Remove(tenant tenancy.Tenant, id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.images, key{tenant.ID, id})
}

// Resolve implements richtext.Files for conformance tests and consumers.
func (f *FakeFiles) Resolve(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID, audience richtext.Audience) (richtext.Image, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.Err != nil {
		return richtext.Image{}, f.Err
	}
	e, ok := f.images[key{db.TenantOf(tx).ID, id}]
	if !ok || (audience == richtext.Public && !e.public) {
		return richtext.Image{}, richtext.ErrMissing
	}
	return e.image, nil
}
