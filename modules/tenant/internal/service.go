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

// AddHost gives an existing tenant another name to answer at, and says whether
// it is the one to name. Three answers, in the order the command reaches them:
// the host is already here and nobody asked for it to be primary, so nothing
// changed and nothing is said; the host is already here and is already the
// primary one, which is the same nothing asked twice, and a retry of a verb that
// wrote no column must not put a second act in two trails; and anything else is a
// change to the routing table — a new row, or the primary moving to a name that
// was already there — which writes, publishes `tenant.host_added` in both trails,
// and therefore asks the audit question before it writes.
//
// "Make this the primary" is a thing somebody may ask about a host that is
// already here, and that promotion is the change the route's own description names:
// the primary host is what every absolute URL for this tenant is built on. It moves
// a column rather than adding a row, which is the only reason it is a different
// branch; it is recorded the way the arrival is recorded, because the trail that
// cannot say which name a tenant's links moved to, or when, is the trail this
// module's own rule refuses to leave an operator with.
func (s *Service) AddHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string, primary bool) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	key := httpx.HostOnly(host)
	// Which of the three answers this is, decided from the one read the command
	// already holds: `t.Hosts` comes back in hostOrder, so its first element is the
	// primary host and RemoveHost reads the same fact the same way.
	known := slices.Contains(t.Hosts, key)
	if known && (!primary || t.Hosts[0] == key) {
		return t, nil
	}
	// Asked before either branch writes, like every other command asks it: a verb
	// that cannot audit both sides writes neither, and an INSERT — or a promotion —
	// kept ahead of this call would put the refusal back into the caller's good
	// behaviour, which leaves a hostname attached by a command that refused.
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	if known {
		if err := s.promote(tx, t.ID, key); err != nil {
			return nil, err
		}
	} else if err := s.attach(tx, t, host, primary); err != nil {
		return nil, err
	}
	// One event for both branches, and `primary` says which change it records: the
	// name arrived, or the name that was here took the routing table.
	err = s.record(ctx, tx, t, operator, verbAddHost, contracts.EventHostAdded, contracts.HostAdded{
		TenantID: t.ID, Host: key, Primary: primary, At: db.Now(),
	})
	if err != nil {
		return nil, err
	}
	// Read back rather than patched in Go: what this returns is the host list
	// as the database orders it, which is the list every caller will see next
	// time. A value assembled here would agree with the table only for as long
	// as two orderings happened to match.
	return s.Get(ctx, tx, id)
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

// RemoveHost stops serving one name. Two floors refuse, and each names the verb
// that would lift it: the primary host is what every absolute URL for this tenant
// is built on, so promote another one first; and a tenant's last host is the
// sentence Create's own comment uses about a tenant nothing routes to, so add the
// replacement before removing the one people are using.
//
// A host the tenant does not answer at changes nothing and publishes nothing.
// Once the row is gone there is nothing left to tell "never yours" from "yours and
// gone", and the answer that does not leak who serves that name now is the one
// that is also safe to retry.
func (s *Service) RemoveHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	key := httpx.HostOnly(host)
	if !slices.Contains(t.Hosts, key) {
		return t, nil
	}
	if len(t.Hosts) == 1 {
		return nil, fmt.Errorf("%w: %q is %s's only host; add the replacement with add-host before removing the one people reach this tenant at",
			crud.ErrConflict, key, t.Slug)
	}
	if t.Hosts[0] == key {
		return nil, fmt.Errorf("%w: %q is %s's primary host, the name every absolute URL for this tenant is built on; make another host primary with add-host first",
			crud.ErrConflict, key, t.Slug)
	}
	// Asked before the row is deleted, like every other command asks it: a verb that
	// cannot audit both sides writes neither, and a DELETE kept ahead of this call
	// would put the refusal back into the caller's good behaviour.
	operator, err := s.audience(tx, t)
	if err != nil {
		return nil, err
	}
	if err := tx.DB().Exec("DELETE FROM tenant_hosts WHERE tenant_id = ? AND host = ?", t.ID, key).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if err := s.record(ctx, tx, t, operator, verbRemoveHost, contracts.EventHostRemoved, contracts.HostRemoved{
		TenantID: t.ID, Host: key, At: db.Now(),
	}); err != nil {
		return nil, err
	}
	// Read back, as AddHost does: the host list this returns is the table's, in
	// the order the table orders it.
	return s.Get(ctx, tx, id)
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

// installation is the operator tenant's id and slug — the scope every lifecycle
// verb mirrors its row into. The predicate is the one the partial unique index
// tenants_operator serves, and the read is inside the writing transaction rather
// than cached at boot, because which tenant is the installation's is a row and not
// a build flag.
//
// Answering none is a refusal, not an empty scope: a verb that cannot write both
// audit rows writes neither. Every request that could reach one of these commands
// was authorized at the operator tenant's own host, so the state this names is an
// installation that is not installed — unreachable, and worth one query to say so
// rather than to audit from one side.
func (s *Service) installation(tx db.Tx[db.System]) (uuid.UUID, error) {
	var ids []uuid.UUID
	err := tx.DB().Table("tenants").Where("operator AND deleted_at IS NULL").Limit(1).Pluck("id", &ids).Error
	if err != nil {
		return uuid.Nil, crud.Classify(err)
	}
	if len(ids) == 0 {
		return uuid.Nil, contracts.ErrNoOperatorTenant
	}
	return ids[0], nil
}

// SetLocale says which languages one tenant is served in, and which of them is the
// one to fall back to. Both halves are written in the caller's transaction and one
// event says so, because a tenant whose default is not in its own set is a request
// answered in a language that tenant does not serve.
func (s *Service) SetLocale(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, in contracts.SetLocale) (*contracts.Tenant, error) {
	wanted, err := s.validLocales(in)
	if err != nil {
		return nil, err
	}
	t, err := s.lock(tx, id)
	if err != nil {
		return nil, err
	}
	if t.DefaultLocale == wanted[0] && slices.Equal(t.Locales, wanted[1:]) {
		return t, nil
	}
	t.DefaultLocale, t.UpdatedAt = wanted[0], db.Now()
	if err := tx.DB().Model(t).Select("default_locale", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if err := tx.DB().Exec("DELETE FROM tenant_locales WHERE tenant_id = ?", t.ID).Error; err != nil {
		return nil, crud.Classify(err)
	}
	if err := s.serve(tx, t.ID, wanted...); err != nil {
		return nil, err
	}
	// The same argument as a suspension's: the host resolution believes a tenant
	// for half a minute, and the languages of a page are not the languages of the
	// page a person is looking at once the set behind it changed.
	err = events.PublishFor(ctx, tx, t.ID, contracts.EventLocaleSet, contracts.LocaleSet{
		TenantID: t.ID, Default: wanted[0], Supported: wanted[1:], At: t.UpdatedAt,
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, tx, id)
}

// validLocales is the command's rule, in one place: every tag is a tag, every tag is
// one this installation has copy for, the default is the first thing returned, and
// the set behind it is sorted, deduplicated and without the default — which is the
// shape contracts.Tenant's two fields hold them in, so the comparison above that
// decides "nothing changed" is one comparison and not a set operation in three
// places.
//
// The second condition is the one that stops a page lying about its language: a
// tenant served in a language no catalogue carries is a page that declares that
// language and shows the source copy, which is what a browser, a screen reader and
// a translation tool are then told. The installation knows which languages it can
// answer in — it is the composition that read the files — so the composition says,
// and this command refuses anything else rather than finding out at render time.
func (s *Service) validLocales(in contracts.SetLocale) ([]string, error) {
	spoken := s.spoken()
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

// Hosts are the names the transaction's own tenant is served at, the primary one
// first.
//
// One query, under the tenant's own policy: tenant_hosts lets a tenant
// transaction see its own rows and nothing else, so this needs no capability
// and grants none. The order is the same as everywhere else here, so "the first
// host" means the same thing to a mailed link as it does to a screen — and it
// now means something a person chose, rather than whichever name sorts first.
func (s *Service) Hosts(_ context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	var hosts []string
	err := tx.DB().Table("tenant_hosts").Order(hostOrder).Pluck("host", &hosts).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return hosts, nil
}

// hostOrder is the one order a host list is ever read in: the primary first,
// then the rest by name. It is a constant because "the first host" is a
// promise three callers make — a mailed link, a screen, and the tenant a
// control-plane response describes — and three ORDER BY clauses that drifted
// would be three different first hosts.
const hostOrder = "is_primary DESC, host"

// attach writes one host row and adds it to the tenant in hand.
func (s *Service) attach(tx db.Tx[db.System], t *contracts.Tenant, host string, primary bool) error {
	key := httpx.HostOnly(host)
	if primary {
		// The old one first: the partial unique index allows one per tenant, so
		// two INSERTs in the other order is a constraint violation rather than
		// a promotion.
		if err := s.demote(tx, t.ID); err != nil {
			return err
		}
	}
	err := tx.DB().Exec("INSERT INTO tenant_hosts (host, tenant_id, is_primary) VALUES (?, ?, ?)",
		key, t.ID, primary).Error
	if err != nil {
		return crud.Classify(err)
	}
	t.Hosts = append(t.Hosts, key)
	return nil
}

// promote makes one of a tenant's existing hosts the primary one, and demote
// unmakes whichever held it. They are two statements because the index allows
// one primary per tenant and an UPDATE that set the new one first would collide
// with the old.
func (s *Service) promote(tx db.Tx[db.System], id uuid.UUID, host string) error {
	if err := s.demote(tx, id); err != nil {
		return err
	}
	err := tx.DB().Exec("UPDATE tenant_hosts SET is_primary = true WHERE tenant_id = ? AND host = ?",
		id, host).Error
	if err != nil {
		return crud.Classify(err)
	}
	return nil
}

func (s *Service) demote(tx db.Tx[db.System], id uuid.UUID) error {
	err := tx.DB().Exec("UPDATE tenant_hosts SET is_primary = false WHERE tenant_id = ? AND is_primary",
		id).Error
	if err != nil {
		return crud.Classify(err)
	}
	return nil
}

func (s *Service) hostsOf(tx db.Tx[db.System], id uuid.UUID) ([]string, error) {
	var hosts []string
	err := tx.DB().Table("tenant_hosts").Where("tenant_id = ?", id).Order(hostOrder).Pluck("host", &hosts).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return hosts, nil
}

// localesOf is the set of languages a tenant is served in, minus the one it is
// served in by default: the table holds every language including that one, so the
// row is never a list whose first element happens to be the default, and the entity
// keeps the pair the way its two fields hold it.
func (s *Service) localesOf(tx db.Tx[db.System], id uuid.UUID, except string) ([]string, error) {
	var stored []string
	err := tx.DB().Table("tenant_locales").Where("tenant_id = ? AND locale <> ?", id, except).
		Order("locale").Pluck("locale", &stored).Error
	if err != nil {
		return nil, crud.Classify(err)
	}
	return stored, nil
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

// spoken is the set of languages this installation can answer in, canonicalised, or
// nil when the composition declared none and there is therefore nothing to check a
// tenant's choice against. An entry that is not a tag is left out rather than
// trusted: it names a language no catalogue file could be read for, and a tenant
// asking for it is refused by the same rule.
func (s *Service) spoken() map[string]bool {
	if len(s.langs) == 0 {
		return nil
	}
	out := make(map[string]bool, len(s.langs))
	for _, tag := range s.langs {
		if canonical, err := contracts.ValidLocale(tag); err == nil {
			out[canonical] = true
		}
	}
	return out
}

// readDefaultLocale takes the language the database just chose for a tenant it
// created. The column's default is the deployment's decision and this module never
// writes it on a create, so the value has to come back rather than be assumed.
func (s *Service) readDefaultLocale(tx db.Tx[db.System], t *contracts.Tenant) error {
	var tag string
	err := tx.DB().Table("tenants").Where("id = ?", t.ID).Pluck("default_locale", &tag).Error
	if err != nil {
		return crud.Classify(err)
	}
	t.DefaultLocale = tag
	return nil
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
