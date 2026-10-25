// Package internal is every implementation of the tenant module. Nothing
// outside modules/tenant can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// The verbs, in the control plane's own words. They are the strings
// contracts.LifecycleRecorded carries, so the operator's trail names the verb a
// route performed rather than a number somebody has to look up; the route ids in
// internal/handler.go are the same words, which is what makes one request
// traceable from the access log to the trail row it left in two tenants.
const (
	verbCreate         = "create"
	verbRename         = "rename"
	verbAddHost        = "add-host"
	verbRemoveHost     = "remove-host"
	verbSuspend        = "suspend"
	verbReactivate     = "reactivate"
	verbDelete         = "delete"
	maxTenantNameRunes = 200
)

// Service is the control plane. Its one field is the list of things main asked
// to happen inside a create, which is how the modules above this one are
// notified without this one importing them (see contracts.Hook).
type Service struct {
	hooks []contracts.Hook
	// langs are the languages this installation's catalogues answer in, and the
	// most a tenant may ever be served in: SetLocale chooses from them, because a
	// language nobody wrote copy for is a page that declares a tongue it does not
	// speak. Empty means the composition named none, and then nothing is checked
	// against it. See module.Deps.
	langs []string
	// app is the composition this control plane serves, and every read below is
	// scoped to it. A server hosts many apps over one database (decision 0074 §6),
	// so tenants of two compositions sit in one table and the tenant id says nothing
	// about which is whose — lookup by host, the active-tenant list and a get by id
	// all have to answer "which app is this happening in" first, or one app's
	// operator lists another's customers. The empty Name is the deployment of one
	// app, whose tenants the migration placed under the empty slug.
	app appname.Name
}

// NewService returns the control plane. module.go constructs it, passing the
// languages the composition's catalogues answer in and the slug the composition
// boots as.
func NewService(hooks []contracts.Hook, langs []string, app appname.Name) *Service {
	return &Service{hooks: hooks, langs: langs, app: app}
}

var _ contracts.Service = (*Service)(nil)

// Create writes the tenant, its first host and whatever the hooks add, all in
// the caller's transaction, so an installation is either whole or absent.
func (s *Service) Create(ctx context.Context, tx db.Tx[db.System], in contracts.NewTenant) (*contracts.Tenant, error) {
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
	// Asked for before the row exists, so a create that cannot be audited from both
	// sides writes no tenant at all: there is no operator tenant to ask in an
	// installation that has never been bootstrapped, and that is exactly the case
	// Bootstrap exists for — the tenant it creates is the operator's own, and needs
	// no mirror.
	operator := uuid.Nil
	if !in.Operator {
		var err error
		if operator, err = s.installation(tx); err != nil {
			return nil, err
		}
	}
	at := db.Now()
	t := &contracts.Tenant{
		ID: uuid.New(), Slug: slug, Name: in.Name, Status: contracts.StatusActive,
		// This composition, and no other: the row is written under the app that
		// created it and never rewritten, which is what makes every read below a
		// boundary rather than a filter. A tenant whose host the boot does not
		// declare is somebody else's tenant, and this create cannot make it ours.
		App: s.app.String(),
		// Never from a request body: NewTenant.Operator is json:"-", so the
		// only caller that can set it is Bootstrap.
		Operator:  in.Operator,
		Demo:      in.Demo,
		CreatedAt: at, UpdatedAt: at,
	}
	if err := tx.DB().Omit("Hosts", "Locales", "DefaultLocale").Create(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	// The language this tenant is served in is the one the installation's
	// catalogues are written in, and this module does not name it: the column's
	// default is the deployment's decision (migrations/000029), read back here so
	// the row the create returns says what the table says. Somebody setting other
	// languages for this tenant is SetLocale, and a create that took them from a
	// body would put the choice in the hands of whoever called the route.
	if err := s.readDefaultLocale(tx, t); err != nil {
		return nil, err
	}
	// Written from the value the database just chose rather than from a constant
	// this module would have to keep in step with the migration, and it is the only
	// language written here. The set behind it is a declaration, and on a create
	// nobody has made one. A tenant whose row predates the column is left with this
	// one language too, by the other half of the same rule: migrations/000029 writes
	// no rows at all and `localesOf` takes the default out of the set on the way
	// read, so the column alone answers for a tenant that has no row beside it, and a
	// tenant created today and a tenant created before that file are served in the
	// same one language. Handing a new tenant the installation's whole set would make
	// the two disagree about two identical tenants, and it would answer a browser in a
	// language whose only author was a composition.
	// SetLocale is where a tenant's people start being served in a second language.
	if err := s.serve(tx, t.ID, t.DefaultLocale); err != nil {
		return nil, err
	}
	// The first host is the primary one, because it is the only one: a tenant
	// whose links pointed nowhere until somebody remembered to choose would be
	// a tenant created half way.
	if err := s.attach(tx, t, host, true); err != nil {
		return nil, err
	}
	for _, hook := range s.hooks {
		if err := hook(ctx, tx, t); err != nil {
			return nil, fmt.Errorf("tenant: %s: %w", t.Slug, err)
		}
	}
	return t, s.record(ctx, tx, t, operator, verbCreate, contracts.EventCreated, contracts.Created{
		TenantID: t.ID, Slug: t.Slug, Name: t.Name, Host: host, At: at,
	})
}

// Suspend stops the tenant being served. Suspending it again changes nothing
// and says nothing: an operator's retry must not appear twice in an audit.
//
// The installation's own tenant is refused. That floor is why this command reads
// the row before it sets a column: `operator` (migrations/000012) is the fact
// that stands between a customer's administrator and the control plane, and
// httpx resolves a request's host to a tenant before it asks who is calling.
// Suspend the operator tenant and every route in this module — the only door
// through which the verb could be asked — starts answering ErrNoSuchHost, with no
// installation left standing to undo it at. 000012's partial index contemplates
// the row being deleted; the verb does not.
func (s *Service) Suspend(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if t.Operator {
		return nil, fmt.Errorf("%w: %q is this installation's own tenant; suspending it closes the control plane it is reached through",
			crud.ErrConflict, t.Slug)
	}
	if t.Status == contracts.StatusSuspended {
		return t, nil
	}
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	t.Status, t.UpdatedAt = contracts.StatusSuspended, db.Now()
	// The two columns this changed, and no others: writing the whole row would
	// put every field back to what this transaction read.
	if err := tx.DB().Model(t).Select("status", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return t, s.record(ctx, tx, t, operator, verbSuspend, contracts.EventSuspended, contracts.Suspended{
		TenantID: t.ID, Slug: t.Slug, At: t.UpdatedAt,
	})
}

// Rename changes what a tenant is called, and nothing else. The slug, the hosts,
// the languages and the id stay where they were: a display name is the one field
// of a tenant a person may change their mind about, and the fields that are DNS
// labels or primary keys are not that field.
func (s *Service) Rename(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.Rename) (*contracts.Tenant, error) {
	name := strings.TrimSpace(in.Name)
	// Refused rather than trimmed into place, so the body and the row can never
	// disagree: the empty case and the over-long one are the same rule seen from
	// either end.
	if name == "" || name != in.Name || utf8.RuneCountInString(name) > maxTenantNameRunes {
		return nil, fmt.Errorf("%w: a tenant's name is 1 to %d characters of display name, not %q",
			crud.ErrInvalid, maxTenantNameRunes, in.Name)
	}
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if t.Name == name {
		return t, nil
	}
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	from, at := t.Name, db.Now()
	t.Name, t.UpdatedAt = name, at
	if err := tx.DB().Model(t).Select("name", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return t, s.record(ctx, tx, t, operator, verbRename, contracts.EventRenamed, contracts.Renamed{
		TenantID: t.ID, From: from, To: name, At: at,
	})
}

// Reactivate resumes serving a suspended tenant. A tenant already being served
// changes nothing and says nothing. A deleted tenant is not found rather than
// refused: `deleted_at` is the other axis and this verb does not clear it, so the
// answer a caller gets is the one every other read of that tenant already gives.
func (s *Service) Reactivate(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if t.Status == contracts.StatusActive {
		return t, nil
	}
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	t.Status, t.UpdatedAt = contracts.StatusActive, db.Now()
	if err := tx.DB().Model(t).Select("status", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return t, s.record(ctx, tx, t, operator, verbReactivate, contracts.EventReactivated, contracts.Reactivated{
		TenantID: t.ID, Slug: t.Slug, At: t.UpdatedAt,
	})
}

// Delete retires a tenant. The write on the row is one column — `deleted_at` — and
// every row the tenant owns stays where it is, because a customer's history is not
// erased by an operator's decision to stop serving them. What the delete releases
// are the two names the platform routes on, and nothing else:
//
// The slug, because the partial unique index of migrations/000006 keeps it only
// while deleted_at IS NULL, so it can be handed to a new customer later while the
// old tenant stays invisible to every reader meanwhile — Get, List, ByHost and
// Active all filter on that column, which is what makes four readers of a
// half-written column into four readers of a verb.
//
// And the hosts, by removing the tenant_hosts rows. `tenant_hosts.host` is a global
// PRIMARY KEY: this is the table that says which tenant a request belongs to before
// there is a tenant-scoped transaction to ask, and it can only answer about a
// tenant that is served. A retired tenant that kept its rows would reserve every
// hostname it ever had forever — `RemoveHost` reaches a tenant through `lock`,
// which filters deleted_at and answers not-found, and `AddHost` for the name would
// be a primary-key violation against a row no reader can find. A hostname owned by
// a customer nobody can see is not history, it is an outage for the next customer.
// What is kept is what only that tenant reads: its rows in every tenant-scoped
// table, its languages, and `contracts.Deleted`, which names the released hosts so
// the trail records which names stopped resolving at which moment — the routing
// table is a routing table, and the pairing lives in the audit from here on.
//
// `t.Hosts` is left as `lock` read it for the same reason: the route invalidates
// exactly these cached resolutions, and a retired tenant is never read back, so the
// list this returns describes the retirement rather than the present. A restore —
// which this module does not have — would have to re-attach a host before the
// tenant it returns could be signed into.
//
// The confirmation is what makes this verb mountable at all: a control-plane POST
// that ends a customer has to be asked for twice, in two different shapes.
//
// A retired tenant is not found, by this verb as by every read: that is what the
// column means, and a retry that answered "already done" would be a reader that
// admits it can still see a customer nobody else can.
func (s *Service) Delete(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.Delete) (*contracts.Tenant, error) {
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(in.Confirm), t.Slug) {
		return nil, fmt.Errorf("%w: deleting %s wants its slug repeated in confirm, not %q",
			crud.ErrInvalid, t.Slug, in.Confirm)
	}
	if t.Operator {
		return nil, fmt.Errorf("%w: %q is this installation's own tenant; deleting it closes the control plane it is reached through",
			crud.ErrConflict, t.Slug)
	}
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	at := db.Now()
	t.DeletedAt, t.UpdatedAt = &at, at
	if err := tx.DB().Model(t).Select("deleted_at", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	// Tenants first, tenant_hosts after, in the one order every command here takes
	// them, and after the column: a delete that failed to release a name would have
	// retired the tenant and be retried, where the reverse would have orphaned one.
	if err := tx.DB().Exec("DELETE FROM tenant_hosts WHERE tenant_id = ?", t.ID).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return t, s.record(ctx, tx, t, operator, verbDelete, contracts.EventDeleted, contracts.Deleted{
		TenantID: t.ID, Slug: t.Slug, Hosts: slices.Clone(t.Hosts), At: at,
	})
}

// lock reads a live tenant's row with the row lock held until the caller's
// transaction ends, and loads its hosts and languages the way Get does.
//
// Every command that compares a tenant's state before it writes takes this lock,
// and the reason is the isolation level rather than the query: kit/db asks
// Postgres for none, so a command runs under READ COMMITTED, where two
// transactions that both read `active` would both write and both publish. Under
// this lock the second one waits for the first, then re-reads the row as the
// first committed it and takes its own idempotent branch — which is what makes
// "suspending twice says nothing the second time" true of a race and not only of
// a test.
//
// crud.GetForUpdate is the same mechanism one tier up; it is typed for a tenant's
// own rows, whose policy would hide every other tenant from a control-plane
// command, so the control plane states its own FOR UPDATE. The order does not
// change: tenants first, tenant_hosts after, which is the order attach and
// promote already take them in.
//
// The app joins the WHERE for the same reason it joins Get's: a lock is a read, and
// a control plane reads inside its own app. Without it this is the one door that
// ignores tenants.app — a verb of another app would find the row, take the lock, and
// write, while every read the same service makes calls that tenant absent. The row
// lock is also where the boundary has to be stated: the verbs that compare a state
// before writing take this read rather than Get, so filtering above it or below it
// would leave the locked read itself app-blind.
func (s *Service) lock(tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	var t contracts.Tenant
	err := tx.DB().Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND app = ? AND deleted_at IS NULL", id, s.app.String()).Take(&t).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return s.loaded(tx, &t)
}

// loaded attaches the two lists a Tenant is only whole with, in the one order
// every read uses.
func (s *Service) loaded(tx db.Tx[db.System], t *contracts.Tenant) (*contracts.Tenant, error) {
	hosts, err := s.hostsOf(tx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Hosts = hosts
	if t.Locales, err = s.localesOf(tx, t.ID, t.DefaultLocale); err != nil {
		return nil, err
	}
	return t, nil
}

// record publishes one lifecycle verb, in both of the two trails it belongs to.
//
// The verb event goes in the subject tenant's scope, as it always has: that is
// the customer's own trail, and the row is there for whoever reads a tenant's
// history back. Beside it goes the mirror in the *installation's* scope, which is
// what an operator's audit of the control plane reads — the row that says "this
// installation renamed, suspended, deleted that customer", and the one that is
// still readable after the customer's own trail has been retained away or had its
// tenant deleted. Both are written in the caller's transaction, so neither exists
// without the column that caused it.
//
// When the subject *is* the installation's own tenant the verb event is the whole
// audit: the two rows would name one fact in one trail, and nothing but this
// branch stops the duplicate, because the trail's idempotence key is
// (tenant_id, event_id) and these are two event ids.
func (s *Service) record(ctx context.Context, tx db.Tx[db.System], t *contracts.Tenant,
	operator uuid.UUID, verb, name string, payload any) error {
	if err := events.SucceedFor(ctx, tx, db.Now()); err != nil {
		return err
	}
	if err := events.PublishFor(ctx, tx, t.ID, name, payload); err != nil {
		return err
	}
	if operator == uuid.Nil {
		return nil
	}
	return events.PublishFor(ctx, tx, operator, contracts.EventLifecycleRecorded,
		contracts.LifecycleRecorded{Verb: verb, TenantID: t.ID, Slug: t.Slug, At: db.Now()})
}

// audience is installation, unless the subject of the verb *is* the installation:
// then there is one trail for both rows, uuid.Nil says "publish the verb and nothing
// beside it", and nothing but this branch stops the duplicate row — the trail's
// idempotence key is (tenant_id, event_id), and these are two event ids.
//
// The question is asked before the command writes, so that the refusal of an
// unauditable verb writes nothing on the caller's transaction's good behaviour.
func (s *Service) audience(tx db.Tx[db.System], subject *contracts.Tenant) (uuid.UUID, error) {
	if subject.Operator {
		return uuid.Nil, nil
	}
	return s.installation(tx)
}

// installation is this app's operator tenant — the scope every lifecycle verb of
// this app mirrors its row into. The predicate is the one the partial unique index
// tenants_operator serves, and the read is inside the writing transaction rather
// than cached at boot, because which tenant is the installation's is a row and not
// a build flag.
//
// It is scoped to the app like every other read here, and that is the whole of the
// read's shape: migrations/000043 made tenants_operator unique on (app) rather than
// on the constant true, so a database of many compositions holds one operator each,
// and an unscoped `LIMIT 1` answers with whichever operator the table happens to
// return first. The row an app mirrors into is the one whose audit its own operator
// reads and whose app its own relay claims (kit/events' RelayApp claims an outbox
// row by the tenant's app), so mirroring into another app's installation puts the
// verb in a trail its own installation will never read — and names a customer that
// trail has never seen.
//
// Answering none is a refusal, not an empty scope: a verb that cannot write both
// audit rows writes neither, and another app's operator is not this app's
// installation. Every request that could reach one of these commands was authorized
// at the operator tenant's own host, so the state this names is an installation that
// is not installed — unreachable, and worth one query to say so rather than to audit
// from one side.
func (s *Service) installation(tx db.Tx[db.System]) (uuid.UUID, error) {
	var ids []uuid.UUID
	err := tx.DB().Table("tenants").
		Where("operator AND app = ? AND deleted_at IS NULL", s.app.String()).Limit(1).Pluck("id", &ids).Error
	if err != nil {
		return uuid.Nil, crud.Classify(err)
	}
	if len(ids) == 0 {
		return uuid.Nil, contracts.ErrNoOperatorTenant
	}
	return ids[0], nil
}

// Get is one tenant with its hosts.
func (s *Service) Get(_ context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	var t contracts.Tenant
	if err := tx.DB().Where("id = ? AND app = ? AND deleted_at IS NULL", id, s.app.String()).Take(&t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return s.loaded(tx, &t)
}

// List is every tenant of this app that is not deleted, with its hosts. The hosts
// come back
// in one query rather than one per tenant, because the control plane's list is
// read by a screen and a screen that costs a query per row is a screen nobody
// keeps.
func (s *Service) List(_ context.Context, tx db.Tx[db.System]) ([]*contracts.Tenant, error) {
	var out []*contracts.Tenant
	if err := tx.DB().Where("deleted_at IS NULL AND app = ?", s.app.String()).Order("created_at, id").Find(&out).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if len(out) == 0 {
		return out, nil
	}
	var rows []struct {
		TenantID uuid.UUID
		Host     string
	}
	if err := tx.DB().Table("tenant_hosts").Order("tenant_id, " + hostOrder).Find(&rows).Error; err != nil {
		return nil, crud.Classify(err)
	}
	byTenant := map[uuid.UUID][]string{}
	for _, r := range rows {
		byTenant[r.TenantID] = append(byTenant[r.TenantID], r.Host)
	}
	var languages []struct {
		TenantID uuid.UUID
		Locale   string
	}
	if err := tx.DB().Table("tenant_locales").Order("tenant_id, locale").Find(&languages).Error; err != nil {
		return nil, crud.Classify(err)
	}
	spoken := map[uuid.UUID][]string{}
	for _, r := range languages {
		spoken[r.TenantID] = append(spoken[r.TenantID], r.Locale)
	}
	for _, t := range out {
		t.Hosts = byTenant[t.ID]
		t.Locales = without(spoken[t.ID], t.DefaultLocale)
	}
	return out, nil
}

// ByHost is httpx.TenantLoader: the query every request makes before it is a
// request. A suspended tenant answers ErrNoSuchHost rather than a refusal,
// because from outside a site that is not served and a site that does not exist
// are the same fact, and saying which is telling a stranger about a customer.
func (s *Service) ByHost(_ context.Context, tx db.Tx[db.System], host string) (tenancy.Tenant, error) {
	var t contracts.Tenant
	err := tx.DB().Table("tenants").Select("tenants.*").
		Joins("JOIN tenant_hosts ON tenant_hosts.tenant_id = tenants.id").
		Where("tenant_hosts.host = ? AND tenants.status = ? AND tenants.app = ? AND tenants.deleted_at IS NULL",
			httpx.HostOnly(host), contracts.StatusActive, s.app.String()).
		Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	if err != nil {
		return tenancy.Tenant{}, fmt.Errorf("tenant: resolve %q: %w", host, err)
	}
	// The languages come along with the row rather than being resolved from
	// somewhere else per request: this is the one read every request makes, and the
	// set of languages a page may answer in is exactly as much a fact about the
	// tenant as its name is. It is one further SELECT on tenant_locales inside the
	// resolution — the row and its set are two tables — and the resolution is
	// cached for the host cache's lifetime, which is why the set is invalidated
	// with the row when either changes.
	if t.Locales, err = s.localesOf(tx, t.ID, t.DefaultLocale); err != nil {
		return tenancy.Tenant{}, err
	}
	return t.Tenancy(), nil
}

// without is a set with one entry taken out of it — the shape contracts.Tenant
// keeps the pair in, so a read never has to remember which of the two is stored
// twice.
func without(set []string, one string) []string {
	out := make([]string, 0, len(set))
	for _, tag := range set {
		if tag != one {
			out = append(out, tag)
		}
	}
	return out
}

// serve writes the languages one tenant is served in, replacing whatever was there.
// The default is the first value passed, which is the only one with a column of its
// own; the rest is the set Accept-Language is intersected with.
func (s *Service) serve(tx db.Tx[db.System], id uuid.UUID, locales ...string) error {
	for _, tag := range locales {
		if err := tx.DB().Exec("INSERT INTO tenant_locales (tenant_id, locale) VALUES (?, ?)", id, tag).Error; err != nil {
			return crud.Classify(err)
		}
	}
	return nil
}
