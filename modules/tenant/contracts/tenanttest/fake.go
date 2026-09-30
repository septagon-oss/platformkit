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

	// Installation mirrors the catalogues a real composition hands the module — the
	// languages the installation answers in, which are the most a tenant may ever be
	// served in. It does not decide what a new tenant is served in: the real create
	// reads the column default back from the database, and so does this.
	Installation []string
}

// InstallationLanguages is the set the suite is written against: the fake carries it
// and internal/service_test.go builds the real service with it, so the specification
// runs against the dependency a production composition passes rather than against
// nil, which is a service no application composes.
func InstallationLanguages() []string { return []string{"en", "pt-PT"} }

// NewFake returns an empty control plane that speaks the suite's installation.
func NewFake() *Fake {
	return &Fake{tenants: map[uuid.UUID]contracts.Tenant{}, hosts: map[string]uuid.UUID{},
		Installation: InstallationLanguages()}
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
			return nil, &crud.UniqueConflict{Constraint: "tenants_slug"}
		}
	}
	if _, taken := f.hosts[host]; taken {
		f.mu.Unlock()
		return nil, &crud.UniqueConflict{Constraint: "tenant_hosts_pkey"}
	}
	at := db.Now()
	// The language a tenant starts out in is the installation's default, and it is
	// the only one: the real create reads back the column default
	// migrations/000029 ships — the language the copy is written in, which no Go
	// value hands this module — and writes that one row, because a tenant's set is a
	// declaration and a create carries none. This mirrors it exactly, because a fake
	// that seeded a wider set than the real create does lets a case pass the real
	// create cannot reach — and no caller names them either: in.DefaultLocale is not
	// read here on purpose.
	start := "en"
	t := contracts.Tenant{
		ID: uuid.New(), Slug: slug, Name: in.Name, Status: contracts.StatusActive,
		Hosts: []string{host}, DefaultLocale: start,
		CreatedAt: at, UpdatedAt: at,
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

// AddHost mirrors internal.Service.AddHost, primary and all: the list keeps the
// primary host first, which is the same order the real one reads rows in.
func (f *Fake) AddHost(_ context.Context, _ db.Tx[db.System], id uuid.UUID, host string, primary bool) (*contracts.Tenant, error) {
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
		if owner != id {
			f.mu.Unlock()
			return nil, &crud.UniqueConflict{Constraint: "tenant_hosts_pkey"}
		}
		// A host the tenant already answers at. Promoting it is a change and
		// adding it again is not, so only the first says anything.
		if primary {
			t.Hosts = order(t.Hosts, host)
			f.tenants[id] = t
		}
		defer f.mu.Unlock()
		return f.copy(id)
	}
	t.Hosts = order(append(slices.Clone(t.Hosts), host), primaryOf(t.Hosts, host, primary))
	f.tenants[id], f.hosts[host] = t, id
	f.mu.Unlock()
	f.record(contracts.EventHostAdded)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// order is a host list with primary first and the rest by name, which is what
// "is_primary DESC, host" means in the real one.
func order(hosts []string, primary string) []string {
	rest := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h != primary {
			rest = append(rest, h)
		}
	}
	slices.Sort(rest)
	return append([]string{primary}, rest...)
}

// primaryOf is which host is the primary one after this addition: the new one
// when it was asked for, and whichever held it otherwise.
func primaryOf(was []string, host string, primary bool) string {
	if primary || len(was) == 0 {
		return host
	}
	return was[0]
}

// SetLocale mirrors internal.Service.SetLocale: the default first and the set
// behind it sorted, the same two rules the real one applies — that a tag is a tag,
// and that it is one this installation has copy for — and the same silence when
// nothing changed.
func (f *Fake) SetLocale(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, in contracts.SetLocale) (*contracts.Tenant, error) {
	wanted, err := f.validLocales(in)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	t, ok := f.tenants[id]
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	changed := t.DefaultLocale != wanted[0] || !slices.Equal(t.Locales, wanted[1:])
	t.DefaultLocale, t.Locales, t.UpdatedAt = wanted[0], wanted[1:], db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if changed {
		f.record(contracts.EventLocaleSet)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// validLocales is internal.Service.validLocales, kept beside it rather than shared
// because a fake that imported the implementation would stop being a second opinion.
func (f *Fake) validLocales(in contracts.SetLocale) ([]string, error) {
	spoken := f.spoken()
	defaultTag, err := contracts.ValidLocale(in.Default)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	if spoken != nil && !spoken[defaultTag] {
		return nil, fmt.Errorf("%w: %s is not a language this installation has copy for", crud.ErrInvalid, defaultTag)
	}
	set := make([]string, 0, len(in.Supported))
	for _, tag := range in.Supported {
		canonical, err := contracts.ValidLocale(tag)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
		}
		if spoken != nil && !spoken[canonical] {
			return nil, fmt.Errorf("%w: %s is not a language this installation has copy for", crud.ErrInvalid, canonical)
		}
		if canonical != defaultTag && !slices.Contains(set, canonical) {
			set = append(set, canonical)
		}
	}
	slices.Sort(set)
	return append([]string{defaultTag}, set...), nil
}

// spoken is internal.Service.spoken: the installation's languages canonicalised, or
// nil when the composition named none and there is nothing to check a tenant's
// choice against.
func (f *Fake) spoken() map[string]bool {
	if len(f.Installation) == 0 {
		return nil
	}
	out := make(map[string]bool, len(f.Installation))
	for _, tag := range f.Installation {
		if canonical, err := contracts.ValidLocale(tag); err == nil {
			out[canonical] = true
		}
	}
	return out
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
// the one on the context rather than the one a transaction was scoped to. A
// consumer testing a mailed link against this is testing the shape of the
// answer; that it is this tenant's and nobody else's is a claim only Postgres
// can be held to, and modules/tenant's own test holds it.
//
// It read a field a consumer had to set and nobody ever did, so it answered
// nothing to everybody. The context is where the tenant already is.
func (f *Fake) Hosts(ctx context.Context, _ db.Tx[db.Tenant]) ([]string, error) {
	who, ok := tenancy.FromContext(ctx)
	if !ok {
		return nil, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.tenants[who.ID]
	if !ok {
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
