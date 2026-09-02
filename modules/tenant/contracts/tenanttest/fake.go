package tenanttest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Fake is contracts.Service over two maps: the same rules, no database, no
// transaction. A consumer that wants to test what it does when a tenant is
// suspended takes one of these instead of a Postgres.
//
// It ignores the transaction it is handed, and that is its honest limit: it
// cannot tell a caller that a write did not commit, because nothing here
// commits. Everything it can be wrong about is what RunService checks.
type Fake struct {
	mu        sync.Mutex
	tenants   map[uuid.UUID]contracts.Tenant
	hosts     map[string]uuid.UUID
	published []string

	// Hooks are what Create runs, the same list the real module takes in Deps.
	Hooks []contracts.Hook

	// Only is the tenant a tenant-scoped read answers about. The real service
	// takes it from the transaction and the policy; there is no transaction
	// here, so a consumer that wants Hosts to answer says which tenant it is
	// pretending to be.
	Only uuid.UUID
}

// NewFake returns an empty control plane.
func NewFake() *Fake {
	return &Fake{tenants: map[uuid.UUID]contracts.Tenant{}, hosts: map[string]uuid.UUID{}}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted, in order.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Create mirrors internal.Service.Create.
func (f *Fake) Create(ctx context.Context, tx db.Tx[db.System], in contracts.NewTenant) (*contracts.Tenant, error) {
	slug, err := contracts.ValidSlug(in.Slug)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	host, err := contracts.ValidHost(in.Host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if in.Name == "" {
		return nil, fmt.Errorf("%w: a tenant needs a name", crud.ErrInvalid)
	}
	host = lower(host)
	f.mu.Lock()
	for _, t := range f.tenants {
		if t.Slug == slug {
			f.mu.Unlock()
			return nil, fmt.Errorf("%w: tenants_slug", crud.ErrConflict)
		}
	}
	if _, taken := f.hosts[host]; taken {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: tenant_hosts_pkey", crud.ErrConflict)
	}
	at := db.Now()
	t := contracts.Tenant{
		ID: uuid.New(), Slug: slug, Name: in.Name, Status: contracts.StatusActive,
		Hosts: []string{host}, CreatedAt: at, UpdatedAt: at,
	}
	f.tenants[t.ID], f.hosts[host] = t, t.ID
	f.mu.Unlock()

	for _, hook := range f.Hooks {
		if err := hook(ctx, tx, &t); err != nil {
			return nil, err
		}
	}
	f.record(contracts.EventCreated)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(t.ID)
}

// AddHost mirrors internal.Service.AddHost.
func (f *Fake) AddHost(_ context.Context, _ db.Tx[db.System], id uuid.UUID, host string) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	host = lower(host)
	f.mu.Lock()
	t, ok := f.tenants[id]
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if owner, taken := f.hosts[host]; taken {
		f.mu.Unlock()
		if owner == id {
			f.mu.Lock()
			defer f.mu.Unlock()
			return f.copy(id)
		}
		return nil, fmt.Errorf("%w: tenant_hosts_pkey", crud.ErrConflict)
	}
	t.Hosts = append(slices.Clone(t.Hosts), host)
	slices.Sort(t.Hosts)
	f.tenants[id], f.hosts[host] = t, id
	f.mu.Unlock()
	f.record(contracts.EventHostAdded)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// Suspend mirrors internal.Service.Suspend.
func (f *Fake) Suspend(_ context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.tenants[id]
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	already := t.Status == contracts.StatusSuspended
	t.Status, t.UpdatedAt = contracts.StatusSuspended, db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if !already {
		f.record(contracts.EventSuspended)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// Get mirrors internal.Service.Get.
func (f *Fake) Get(_ context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// List mirrors internal.Service.List: every tenant, suspended ones included.
func (f *Fake) List(_ context.Context, _ db.Tx[db.System]) ([]*contracts.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*contracts.Tenant, 0, len(f.tenants))
	for id := range f.tenants {
		t, _ := f.copy(id)
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b *contracts.Tenant) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out, nil
}

// ByHost mirrors internal.Service.ByHost, suspension and all.
func (f *Fake) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.hosts[lower(host)]
	if !ok {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	t := f.tenants[id]
	if t.Status != contracts.StatusActive {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return t.Tenancy(), nil
}

// Hosts is what the real service answers under a tenant transaction, with one
// difference this fake cannot close: there is no policy here, so the tenant is
// the one Only names rather than the one a transaction was scoped to. A
// consumer testing a mailed link against this is testing the shape of the
// answer; that it is this tenant's and nobody else's is a claim only Postgres
// can be held to, and modules/tenant's own test holds it.
func (f *Fake) Hosts(_ context.Context, _ db.Tx[db.Tenant]) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[f.Only]
	if f.Only == uuid.Nil || !ok {
		return nil, nil
	}
	return slices.Clone(t.Hosts), nil
}

// copy is a detached copy, so a caller that mutates what it was handed does not
// reach into the store — which is what a database would do. Where it is called
// without the lock held, the map is not being written.
func (f *Fake) copy(id uuid.UUID) (*contracts.Tenant, error) {
	t, ok := f.tenants[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	t.Hosts = slices.Clone(t.Hosts)
	return &t, nil
}

func (f *Fake) record(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, name)
}

// lower is the host as it is stored: kit/httpx normalises an incoming Host
// header to lower case before it asks a loader, so the key has to match.
func lower(host string) string { return strings.ToLower(host) }
