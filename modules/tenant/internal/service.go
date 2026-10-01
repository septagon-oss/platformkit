// Package internal is every implementation of the tenant module. Nothing
// outside modules/tenant can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
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
}

// NewService returns the control plane. module.go constructs it, passing the
// languages the composition's catalogues answer in.
func NewService(hooks []contracts.Hook, langs []string) *Service {
	return &Service{hooks: hooks, langs: langs}
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
	at := db.Now()
	t := &contracts.Tenant{
		ID: uuid.New(), Slug: slug, Name: in.Name, Status: contracts.StatusActive,
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
	return t, events.PublishFor(ctx, tx, t.ID, contracts.EventCreated, contracts.Created{
		TenantID: t.ID, Slug: t.Slug, Name: t.Name, Host: host, At: at,
	})
}

// AddHost gives an existing tenant another name to answer at, and says whether
// it is the one to name. The same host again is the same tenant and no second
// event — but it is still promoted, because "make this the primary" is a thing
// somebody may ask about a host that is already there.
func (s *Service) AddHost(ctx context.Context, tx db.Tx[db.System], id uuid.UUID, host string, primary bool) (*contracts.Tenant, error) {
	host, err := contracts.ValidHost(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	key := httpx.HostOnly(host)
	if slices.Contains(t.Hosts, key) {
		if !primary {
			return t, nil
		}
		if err := s.promote(tx, t.ID, key); err != nil {
			return nil, err
		}
		return s.Get(ctx, tx, id)
	}
	if err := s.attach(tx, t, host, primary); err != nil {
		return nil, err
	}
	err = events.PublishFor(ctx, tx, t.ID, contracts.EventHostAdded, contracts.HostAdded{
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
func (s *Service) Suspend(ctx context.Context, tx db.Tx[db.System], id uuid.UUID) (*contracts.Tenant, error) {
	t, err := s.Get(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if t.Status == contracts.StatusSuspended {
		return t, nil
	}
	t.Status, t.UpdatedAt = contracts.StatusSuspended, db.Now()
	// The two columns this changed, and no others: writing the whole row would
	// put every field back to what this transaction read.
	if err := tx.DB().Model(t).Select("status", "updated_at").Updates(t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	return t, events.PublishFor(ctx, tx, t.ID, contracts.EventSuspended, contracts.Suspended{
		TenantID: t.ID, Slug: t.Slug, At: t.UpdatedAt,
	})
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
	t, err := s.Get(ctx, tx, id)
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
	if err := tx.DB().Where("id = ? AND deleted_at IS NULL", id).Take(&t).Error; err != nil {
		return nil, crud.Classify(err)
	}
	hosts, err := s.hostsOf(tx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Hosts = hosts
	if t.Locales, err = s.localesOf(tx, t.ID, t.DefaultLocale); err != nil {
		return nil, err
	}
	return &t, nil
}

// List is every tenant that is not deleted, with its hosts. The hosts come back
// in one query rather than one per tenant, because the control plane's list is
// read by a screen and a screen that costs a query per row is a screen nobody
// keeps.
func (s *Service) List(_ context.Context, tx db.Tx[db.System]) ([]*contracts.Tenant, error) {
	var out []*contracts.Tenant
	if err := tx.DB().Where("deleted_at IS NULL").Order("created_at, id").Find(&out).Error; err != nil {
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
		Where("tenant_hosts.host = ? AND tenants.status = ? AND tenants.deleted_at IS NULL",
			httpx.HostOnly(host), contracts.StatusActive).
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
