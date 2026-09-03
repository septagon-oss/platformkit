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
type Service struct{ hooks []contracts.Hook }

// NewService returns the control plane. module.go constructs it.
func NewService(hooks []contracts.Hook) *Service { return &Service{hooks: hooks} }

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
		CreatedAt: at, UpdatedAt: at,
	}
	if err := tx.DB().Omit("Hosts").Create(t).Error; err != nil {
		return nil, crud.Classify(err)
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
	for _, t := range out {
		t.Hosts = byTenant[t.ID]
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
