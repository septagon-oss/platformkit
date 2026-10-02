package tenanttest

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
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
	mu      sync.Mutex
	tenants map[uuid.UUID]contracts.Tenant
	hosts   map[string]uuid.UUID
	oidc    map[uuid.UUID]contracts.OIDCSettings
	log     []publication

	// Hooks are what Create runs, the same list the real module takes in Deps.
	Hooks []contracts.Hook

	// Installation mirrors the catalogues a real composition hands the module — the
	// languages the installation answers in, which are the most a tenant may ever be
	// served in. It does not decide what a new tenant is served in: the real create
	// reads the column default back from the database, and so does this.
	Installation []string

	// Operator is the installation's own tenant — the row Bootstrap creates and
	// the scope every lifecycle verb mirrors its audit row into. It is set by
	// Installed, which is how the suite's fixtures and a consumer's test both
	// describe an installed control plane; a Fake without one is an installation
	// that has not been bootstrapped, and every verb refuses.
	Operator uuid.UUID
}

// publication is one event the fake would have written to the outbox: its name,
// the tenant whose scope it belongs to, and the trace id the request carried.
// The three fields are the three the outbox stores, because the question the
// suite asks of them is "which tenant's trail does this row land in, and which
// request does it name" — and that is a question about both rows, not the one.
type publication struct {
	name  string
	scope uuid.UUID
	trace string
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

// secretRefName is internal's rule, spelled again on purpose: a reference has to
// be a name a deployment can resolve, and in this repository that means an
// environment variable.
var secretRefName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted, in order,
// both trails included: a verb that mirrors itself into the installation's scope
// shows up twice here, on purpose.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.log))
	for _, p := range f.log {
		out = append(out, p.name)
	}
	return out
}

// PublishedScopes is the tenant each event belongs to, in the same order as
// Published. It is what makes "one row in the customer's trail and one in the
// installation's" a case rather than a comment.
func (f *Fake) PublishedScopes() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]uuid.UUID, 0, len(f.log))
	for _, p := range f.log {
		out = append(out, p.scope)
	}
	return out
}

// PublishedTraces is the W3C trace id each event carries, "" where the caller
// brought none. Both rows of one verb are written in one request, so the pair
// joins on this value, which is what the trail will be queried by later.
func (f *Fake) PublishedTraces() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.log))
	for _, p := range f.log {
		out = append(out, p.trace)
	}
	return out
}

// Installed creates the control plane the suite is written against: an empty one
// plus the tenant Bootstrap would have created, which is the scope every verb
// mirrors its audit row into. The event that creation would have published is
// returned to the caller rather than left in the log, because a fixture's own
// setup is not part of what a case asserts.
func Installed() (*Fake, *contracts.Tenant, []string) {
	f := NewFake()
	operator, err := f.Create(context.Background(), db.Tx[db.System]{}, contracts.NewTenant{
		Slug: "installation", Name: "This installation", Host: "ops.example.com", Operator: true,
	})
	if err != nil {
		panic("tenanttest: the fixture cannot create its operator tenant: " + err.Error())
	}
	f.mu.Lock()
	log := f.log
	f.log = nil
	f.mu.Unlock()
	names := make([]string, 0, len(log))
	for _, p := range log {
		names = append(names, p.name)
	}
	return f, operator, names
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
	if f.slugTaken(slug) {
		f.mu.Unlock()
		return nil, &crud.UniqueConflict{Constraint: "tenants_slug"}
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
		// json:"-" on the request type, so the only caller that can set it is the
		// fixture or a Bootstrap — which is the same rule the real create keeps.
		Operator: in.Operator, Hosts: []string{host}, DefaultLocale: start,
		CreatedAt: at, UpdatedAt: at,
	}
	f.tenants[t.ID], f.hosts[host] = t, t.ID
	if t.Operator {
		f.Operator = t.ID
	}
	f.mu.Unlock()

	for _, hook := range f.Hooks {
		if err := hook(ctx, tx, &t); err != nil {
			return nil, err
		}
	}
	f.publish(ctx, t.ID, contracts.EventCreated)
	if err := f.mirror(ctx, t.ID); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(t.ID)
}

// AddHost mirrors internal.Service.AddHost, primary and all: the list keeps the
// primary host first, which is the same order the real one reads rows in.
func (f *Fake) AddHost(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, host string, primary bool) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	host = lower(host)
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if owner, taken := f.hosts[host]; taken {
		if owner != id {
			f.mu.Unlock()
			return nil, &crud.UniqueConflict{Constraint: "tenant_hosts_pkey"}
		}
		// A host the tenant already answers at. Moving the primary onto it is a
		// change and says so the way the arrival says so; adding it again, or
		// promoting the name that is already primary, is not a change and says
		// nothing — the same three answers internal.Service.AddHost gives.
		changed := primary && t.Hosts[0] != host
		if changed {
			t.Hosts = order(t.Hosts, host)
			f.tenants[id] = t
		}
		f.mu.Unlock()
		if changed {
			f.publish(ctx, id, contracts.EventHostAdded)
			if err := f.mirror(ctx, id); err != nil {
				return nil, err
			}
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		return f.copy(id)
	}
	t.Hosts = order(append(slices.Clone(t.Hosts), host), primaryOf(t.Hosts, host, primary))
	f.tenants[id], f.hosts[host] = t, id
	f.mu.Unlock()
	f.publish(ctx, id, contracts.EventHostAdded)
	if err := f.mirror(ctx, id); err != nil {
		return nil, err
	}
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
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	changed := t.DefaultLocale != wanted[0] || !slices.Equal(t.Locales, wanted[1:])
	t.DefaultLocale, t.Locales, t.UpdatedAt = wanted[0], wanted[1:], db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if changed {
		f.publish(ctx, id, contracts.EventLocaleSet)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// SetOIDC mirrors internal.Service.SetOIDC, refusals included: which provider a
// tenant's people sign in against is a rule about the values and not about the
// table they land in, so the fake refuses the same half-providers, for the same
// reason, in the same words.
func (f *Fake) SetOIDC(ctx context.Context, _ db.Tx[db.System], id uuid.UUID, in contracts.OIDCSettings) (*contracts.Tenant, error) {
	if err := validOIDC(in); err != nil {
		return nil, err
	}
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	changed := !sameOIDC(f.oidc[id], in)
	if f.oidc == nil {
		f.oidc = map[uuid.UUID]contracts.OIDCSettings{}
	}
	in.Registration = mode(in)
	f.oidc[id] = in
	t.UpdatedAt = db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if changed {
		f.publish(ctx, id, contracts.EventOIDCSet)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// ClearOIDC mirrors internal.Service.ClearOIDC, including the rule that clearing
// a tenant with no provider changes nothing and publishes nothing.
func (f *Fake) ClearOIDC(ctx context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	_, had := f.oidc[id]
	delete(f.oidc, id)
	t.UpdatedAt = db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if had {
		f.publish(ctx, id, contracts.EventOIDCCleared)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// OIDCOf mirrors internal.Service.OIDCOf, answered from the tenant the
// transaction carries. One tenant per map entry is as far as a fake with no
// policy can model a boundary; the cross-tenant case is internal's to prove.
func (f *Fake) OIDCOf(_ context.Context, tx db.Tx[db.Tenant]) (*contracts.OIDCSettings, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	settings, ok := f.oidc[db.TenantOf(tx).ID]
	if !ok {
		return nil, false, nil
	}
	return &settings, true, nil
}

// mode, sameOIDC and validOIDC are internal.Service's three helpers, kept here
// rather than shared because a fake that imported the implementation would stop
// being a second opinion about it.
func mode(in contracts.OIDCSettings) string {
	if in.Registration == "" {
		return contracts.RegistrationExisting
	}
	return in.Registration
}

func sameOIDC(a, b contracts.OIDCSettings) bool {
	return a.Issuer == b.Issuer && a.ClientID == b.ClientID && a.SecretRef == b.SecretRef &&
		a.RedirectPath == b.RedirectPath && mode(a) == mode(b) && slices.Equal(a.Roles, b.Roles)
}

func validOIDC(in contracts.OIDCSettings) error {
	u, err := url.Parse(in.Issuer)
	switch {
	case err != nil || u.Host == "":
		return fmt.Errorf("%w: oidc.issuer %q is not a URL", crud.ErrInvalid, in.Issuer)
	case u.Scheme != "https" && !config.Local(u.Host):
		return fmt.Errorf("%w: oidc.issuer %q is not https", crud.ErrInvalid, in.Issuer)
	case u.RawQuery != "" || u.Fragment != "":
		return fmt.Errorf("%w: oidc.issuer %q carries a query; an issuer is a base URL", crud.ErrInvalid, in.Issuer)
	case in.ClientID == "":
		return fmt.Errorf("%w: oidc.clientId is empty", crud.ErrInvalid)
	case !secretRefName.MatchString(in.SecretRef):
		return fmt.Errorf("%w: oidc.secretRef %q is not an environment variable's name", crud.ErrInvalid, in.SecretRef)
	}
	switch mode(in) {
	case contracts.RegistrationDisabled, contracts.RegistrationExisting, contracts.RegistrationProvision:
	default:
		return fmt.Errorf("%w: oidc.registration %q is not disabled, existing or provision", crud.ErrInvalid, in.Registration)
	}
	if mode(in) == contracts.RegistrationProvision && len(in.Roles) == 0 {
		return fmt.Errorf("%w: provision with no roles would make people who can do nothing", crud.ErrInvalid)
	}
	return nil
}

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

// Suspend mirrors internal.Service.Suspend, the operator floor included: the
// installation's own tenant is the one tenant nobody may stop serving, because it
// is the tenant every control-plane request is authorized at.
func (f *Fake) Suspend(ctx context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	t, ok := f.live(id)
	if !ok {
		f.mu.Unlock()
		return nil, crud.ErrNotFound
	}
	if t.Operator {
		f.mu.Unlock()
		return nil, fmt.Errorf("%w: %q is this installation's own tenant; suspending it closes the control plane it is reached through",
			crud.ErrConflict, t.Slug)
	}
	already := t.Status == contracts.StatusSuspended
	t.Status, t.UpdatedAt = contracts.StatusSuspended, db.Now()
	f.tenants[id] = t
	f.mu.Unlock()
	if !already {
		f.publish(ctx, id, contracts.EventSuspended)
		if err := f.mirror(ctx, id); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copy(id)
}

// Get mirrors internal.Service.Get.
func (f *Fake) Get(_ context.Context, _ db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.live(id); !ok {
		return nil, crud.ErrNotFound
	}
	return f.copy(id)
}

// List mirrors internal.Service.List: every tenant, suspended ones included.
func (f *Fake) List(_ context.Context, _ db.Tx[db.System]) ([]*contracts.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*contracts.Tenant, 0, len(f.tenants))
	for id := range f.tenants {
		if _, ok := f.live(id); !ok {
			continue
		}
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
	t, ok := f.live(id)
	if !ok || t.Status != contracts.StatusActive {
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
	t, ok := f.live(who.ID)
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

// publish is the fake's half of internal.Service.record: the event in the
// subject tenant's own scope, with the trace id the caller's context carried.
func (f *Fake) publish(ctx context.Context, subject uuid.UUID, name string) {
	c, _ := trace.From(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log = append(f.log, publication{name: name, scope: subject, trace: c.Parent()})
}

// mirror is the other half: the operator's row, or the refusal that says there is
// no installation to write it into. It returns an error rather than staying quiet,
// because "the verb wrote nothing" is the whole point — a fake that dropped the
// mirror would let a caller's code pass that the real service refuses.
//
// The row is read whether or not it is still a tenant: a delete mirrors itself
// after it retired the subject, and the verb's other row is written for a
// customer who can no longer be read.
//
// When the subject is the installation's own tenant there is nothing to mirror:
// the verb event is already in the one trail both rows would have landed in.
func (f *Fake) mirror(ctx context.Context, subject uuid.UUID) error {
	f.mu.Lock()
	t, ok := f.tenants[subject]
	f.mu.Unlock()
	if !ok {
		return crud.ErrNotFound
	}
	if t.Operator {
		return nil
	}
	if f.Operator == uuid.Nil {
		return contracts.ErrNoOperatorTenant
	}
	f.publish(ctx, f.Operator, contracts.EventLifecycleRecorded)
	return nil
}

// lower is the host as it is stored: kit/httpx normalises an incoming Host
// header to lower case before it asks a loader, so the key has to match.
func lower(host string) string { return strings.ToLower(host) }
