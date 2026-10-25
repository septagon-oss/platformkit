package internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

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
