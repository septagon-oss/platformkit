package tenanttest

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The four lifecycle commands the fake mirrors from internal/service.go, and the
// state they read: `deleted_at`, which the entity has carried since
// migrations/000006 and which nothing here wrote until the module gained a verb
// that did.
//
// They live apart from the create/suspend/set-locale trio because they are the
// lifecycle: the transitions a consumer's test most wants a tenant in, and the
// ones whose rules (two floors, a released slug, an idempotent silence) the
// conformance suite exists to keep one answer in two implementations.

// Rename mirrors internal.Service.Rename: the display name and nothing else, and
// the silence when the name it was given is the one the tenant already has.
func (f *Fake) Rename(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, in contracts.Rename) (*contracts.Tenant, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || name != in.Name || len(name) > maxNameRunes {
		return nil, fmt.Errorf("%w: a tenant's name is 1 to %d characters of display name, not %q",
			crud.ErrInvalid, maxNameRunes, in.Name)
	}
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if t.Name == name {
		defer f.mu.Unlock()
		return f.copy(id)
	}
	at := db.Now()
	t.Name, t.UpdatedAt = name, at
	f.tenants[id] = t
	f.mu.Unlock()

	f.publish(ctx, id, contracts.EventRenamed)
	if err := f.mirror(ctx, id); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// Reactivate mirrors internal.Service.Reactivate: the other half of a suspension,
// silent on a tenant that is already served, and answering a retired tenant with
// ErrNotFound rather than resuming it — that is what Restore is for.
func (f *Fake) Reactivate(ctx context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if t.Status == contracts.StatusActive {
		defer f.mu.Unlock()
		return f.copy(id)
	}
	t.Status, t.UpdatedAt = contracts.StatusActive, db.Now()
	f.tenants[id] = t
	f.mu.Unlock()

	f.publish(ctx, id, contracts.EventReactivated)
	if err := f.mirror(ctx, id); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// RemoveHost mirrors internal.Service.RemoveHost, both floors named by the verb
// that lifts them, and the no-op that leaks nothing: once a name is nobody's, the
// answer that does not say who serves it is also the answer that is safe to retry.
func (f *Fake) RemoveHost(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, host string) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	key := lower(host)
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if !slices.Contains(t.Hosts, key) {
		defer f.mu.Unlock()
		return f.copy(id)
	}
	if len(t.Hosts) == 1 {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: %q is %s's only host; add the replacement with add-host before removing the one people reach this tenant at",
			crud.ErrConflict, key, t.Slug)
	}
	if t.Hosts[0] == key {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: %q is %s's primary host, the name every absolute URL for this tenant is built on; make another host primary with add-host first",
			crud.ErrConflict, key, t.Slug)
	}
	t.Hosts = slices.DeleteFunc(slices.Clone(t.Hosts), func(h string) bool { return h == key })
	t.UpdatedAt = db.Now()
	f.tenants[id] = t
	delete(f.hosts, key)
	f.mu.Unlock()

	f.publish(ctx, id, contracts.EventHostRemoved)
	if err := f.mirror(ctx, id); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// Delete mirrors internal.Service.Delete: the confirmation, the operator's floor,
// and the one column. The rows the tenant owns stay — a fake that garbage-collected
// here would let a caller's code pass that the real database cannot reach.
func (f *Fake) Delete(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, in contracts.Delete) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if !strings.EqualFold(strings.TrimSpace(in.Confirm), t.Slug) {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: deleting %s wants its slug repeated in confirm, not %q",
			crud.ErrInvalid, t.Slug, in.Confirm)
	}
	if t.Operator {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: %q is this installation's own tenant; deleting it closes the control plane it is reached through",
			crud.ErrConflict, t.Slug)
	}
	if t.DeletedAt != nil {
		defer f.mu.Unlock()
		return f.copy(id)
	}
	at := db.Now()
	t.DeletedAt, t.UpdatedAt = &at, at
	f.tenants[id] = t
	f.mu.Unlock()
	// The slug is free again, because the unique index in migrations/000006 is
	// partial on deleted_at IS NULL: the name a delete releases is the fact the
	// next customer may have.
	f.publish(ctx, id, contracts.EventDeleted)
	if err := f.mirror(ctx, id); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// live is a tenant that has not been retired — the fake's copy of the
// `deleted_at IS NULL` that every read in the real service carries, and the
// reason Get, List, ByHost and Active all answer the same way about a customer
// somebody deleted. The caller holds the mutex.
func (f *Fake) live(id uuid.UUID) (contracts.Tenant, bool) {
	t, ok := f.tenants[id]
	if !ok || t.DeletedAt != nil {
		return contracts.Tenant{}, false
	}
	return t, true
}

// slugTaken is the partial index: a slug is taken while some live row holds it,
// and a retired tenant stops holding it.
func (f *Fake) slugTaken(slug string) bool {
	for _, t := range f.tenants {
		if t.DeletedAt == nil && t.Slug == slug {
			return true
		}
	}
	return false
}

// maxNameRunes is Rename's ceiling — the same 200 the request body's tag carries
// and the column was written with. internal/service.go has its own copy of the
// number, and RunService is what keeps the two copies one answer: a fake that
// imported the implementation would stop being a second opinion.
const maxNameRunes = 200
