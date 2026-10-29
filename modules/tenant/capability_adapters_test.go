package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
)

type hostsOnly func(context.Context, db.Tx[db.Tenant]) ([]string, error)

func (f hostsOnly) Hosts(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	return f(ctx, tx)
}

type oidcOnly func(context.Context, db.Tx[db.Tenant]) (*contracts.OIDCSettings, bool, error)

func (f oidcOnly) OIDCOf(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.OIDCSettings, bool, error) {
	return f(ctx, tx)
}

type samlOnly func(context.Context, db.Tx[db.Tenant]) (*contracts.SAMLSettings, bool, error)

func (f samlOnly) SAMLSettingsOf(ctx context.Context, tx db.Tx[db.Tenant]) (*contracts.SAMLSettings, bool, error) {
	return f(ctx, tx)
}

type tenantsOnly func(context.Context, db.Tx[db.System]) ([]*contracts.Tenant, error)

func (f tenantsOnly) List(ctx context.Context, tx db.Tx[db.System]) ([]*contracts.Tenant, error) {
	return f(ctx, tx)
}

type bootstrapOnly struct{ fake *tenanttest.Fake }

func (b bootstrapOnly) List(ctx context.Context, tx db.Tx[db.System]) ([]*contracts.Tenant, error) {
	return b.fake.List(ctx, tx)
}
func (b bootstrapOnly) Create(ctx context.Context, tx db.Tx[db.System], in contracts.NewTenant) (*contracts.Tenant, error) {
	return b.fake.Create(ctx, tx, in)
}

func TestPublicHostNeedsOnlyThePrimaryOrderedHostList(t *testing.T) {
	for _, tc := range []struct {
		hosts []string
		want  string
	}{
		{[]string{"z.example.test", "a.example.test"}, "z.example.test:8443"},
		{[]string{"a.example.test", "z.example.test"}, "a.example.test:8443"},
	} {
		calls := 0
		h := tenantHosts{published: "8443", tenants: hostsOnly(func(context.Context, db.Tx[db.Tenant]) ([]string, error) { calls++; return tc.hosts, nil })}
		got, err := h.PublicHost(t.Context(), db.Tx[db.Tenant]{})
		if got != tc.want || err != nil || calls != 1 {
			t.Fatalf("primary host = %q, %v, calls=%d", got, err, calls)
		}
	}
	want := errors.New("host read failed")
	h := tenantHosts{tenants: hostsOnly(func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return nil, want })}
	if host, err := h.PublicHost(t.Context(), db.Tx[db.Tenant]{}); host != "" || !errors.Is(err, want) {
		t.Fatalf("failed host = %q, %v", host, err)
	}
}

func TestProviderAdaptersNeedOnlyTheirOwnProtocolReader(t *testing.T) {
	for _, present := range []bool{false, true} {
		calls := 0
		o := tenantProviders{tenants: oidcOnly(func(context.Context, db.Tx[db.Tenant]) (*contracts.OIDCSettings, bool, error) {
			calls++
			return &contracts.OIDCSettings{Issuer: "https://issuer.example.test", Roles: []string{"member"}}, present, nil
		})}
		p, ok, err := o.ProviderOf(t.Context(), db.Tx[db.Tenant]{})
		if err != nil || ok != present || calls != 1 || present && p.Issuer != "https://issuer.example.test" {
			t.Fatalf("OIDC: %v, %v, %v", p, ok, err)
		}
		calls = 0
		s := samlProviders{tenants: samlOnly(func(context.Context, db.Tx[db.Tenant]) (*contracts.SAMLSettings, bool, error) {
			calls++
			return &contracts.SAMLSettings{EntityID: "https://saml.example.test"}, present, nil
		})}
		sp, ok, err := s.ProviderOf(t.Context(), db.Tx[db.Tenant]{})
		if err != nil || ok != present || calls != 1 || present && sp.EntityID != "https://saml.example.test" {
			t.Fatalf("SAML: %v, %v, %v", sp, ok, err)
		}
	}
}

func TestActiveTenantsNeedsOnlyLister(t *testing.T) {
	active := &contracts.Tenant{ID: uuid.New(), Status: contracts.StatusActive}
	suspended := &contracts.Tenant{ID: uuid.New(), Status: contracts.StatusSuspended}
	calls := 0
	list := contracts.ActiveTenants{Tenants: tenantsOnly(func(context.Context, db.Tx[db.System]) ([]*contracts.Tenant, error) {
		calls++
		return []*contracts.Tenant{active, suspended}, nil
	})}
	got, err := list.List(t.Context(), db.Tx[db.System]{})
	if err != nil || len(got) != 1 || got[0].ID != active.ID || calls != 1 {
		t.Fatalf("active tenants = %v, %v", got, err)
	}
}

func TestBootstrapNeedsOnlyListingAndCreation(t *testing.T) {
	fake := tenanttest.NewFake()
	b := bootstrapOnly{fake: fake}
	first, err := BootstrapWith(t.Context(), db.Tx[db.System]{}, b, contracts.NewTenant{Name: "Operator", Slug: "operator", Host: "operator.example.test"})
	if err != nil || !first.Operator {
		t.Fatalf("first tenant = %v, %v", first, err)
	}
	if again, err := BootstrapWith(t.Context(), db.Tx[db.System]{}, b, contracts.NewTenant{Name: "Second", Slug: "second", Host: "second.example.test"}); err == nil || again != nil {
		t.Fatalf("populated bootstrap = %v, %v", again, err)
	}
}
