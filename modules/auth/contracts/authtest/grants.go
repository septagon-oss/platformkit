package authtest

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// GrantPolicy adapts the existing fake's role store to the shared decision.
// Tenant is an explicit fixture fact; this fake does not simulate SQL transactions or RLS.
type GrantPolicy struct {
	Roles  *Fake
	Tenant tenancy.Tenant
}

func (g GrantPolicy) Allowed(ctx context.Context, tx db.Tx[db.Tenant], want tenancy.Grant) (bool, error) {
	if g.Roles == nil || g.Tenant.ID == uuid.Nil {
		return false, fmt.Errorf("auth fake: roles and tenant are required")
	}
	if current, ok := tenancy.FromContext(ctx); ok && current.ID != g.Tenant.ID {
		return false, fmt.Errorf("auth fake: grant context and fixture tenants differ")
	}
	p, _ := tenancy.PrincipalFrom(ctx)
	if want.Operator && !g.Tenant.Operator {
		p = tenancy.Principal{}
	}
	return contracts.Decide(p, want, func(roles []string) ([]string, error) {
		return g.Roles.Permissions(ctx, tx, roles)
	})
}

// GrantFixture supplies one tenant's authorizer and current role store. Role replaces
// a definition; Published includes all events so the suite can require silent reads.
type GrantFixture struct {
	Ctx       context.Context
	Tx        db.Tx[db.Tenant]
	Policy    contracts.GrantAuthorizer
	Role      func(string, ...string)
	Published func() []string
}

// RunGrants checks the same decisions against an independent SQL reader or fake.
// The harness owns the tenant transaction and cleans up after each case.
func RunGrants(t *testing.T, world func(*testing.T, func(GrantFixture))) {
	porttest.Run(t, porttest.Suite[GrantFixture]{
		Port: "contracts.GrantAuthorizer", World: world,
		Events: func(w GrantFixture) []string { return w.Published() },
		Own: []porttest.Case[GrantFixture]{
			{Name: "principal_and_credential_boundaries", Because: "grant reads depend on principal and credential ceiling, not an entity mutation", Run: grantBoundaries},
			{Name: "changed_roles_are_read_again", Because: "successive decisions must observe a changed role definition", Run: currentGrants},
		},
	})
}

func grantBoundaries(t *testing.T, w GrantFixture) {
	w.Role("editor", "widget:read")
	w.Role("owner", "*")
	id := uuid.New()
	for _, tc := range []struct {
		name      string
		principal tenancy.Principal
		grant     tenancy.Grant
		want      bool
	}{
		{"anonymous", tenancy.Principal{}, tenancy.Grant{Permission: "widget:read"}, false},
		{"zero identity", tenancy.Principal{Roles: []string{"owner"}}, tenancy.Grant{Permission: "widget:read"}, false},
		{"named grant", tenancy.Principal{UserID: id, Roles: []string{"editor"}}, tenancy.Grant{Permission: "widget:read"}, true},
		{"missing grant", tenancy.Principal{UserID: id, Roles: []string{"editor"}}, tenancy.Grant{Permission: "role:manage"}, false},
		{"ordinary wildcard", tenancy.Principal{UserID: id, Roles: []string{"owner"}}, tenancy.Grant{Permission: "widget:write"}, true},
		{"operator wildcard", tenancy.Principal{UserID: id, Roles: []string{"owner"}}, tenancy.Grant{Permission: "fleet:manage", Operator: true}, false},
		{"empty ceiling", tenancy.Principal{UserID: id, Roles: []string{"owner"}, Permissions: []string{}}, tenancy.Grant{Permission: "widget:read"}, false},
		{"limited ceiling", tenancy.Principal{UserID: id, Roles: []string{"owner"}, Permissions: []string{"widget:read"}}, tenancy.Grant{Permission: "role:manage"}, false},
		{"ceiling grant", tenancy.Principal{UserID: id, Permissions: []string{"widget:read"}}, tenancy.Grant{Permission: "widget:read"}, true},
		{"unknown role", tenancy.Principal{UserID: id, Roles: []string{"missing"}}, tenancy.Grant{Permission: "widget:read"}, false},
		{"no roles", tenancy.Principal{UserID: id}, tenancy.Grant{Permission: "widget:read"}, false},
		{"duplicate roles", tenancy.Principal{UserID: id, Roles: []string{"editor", "editor"}}, tenancy.Grant{Permission: "widget:read"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(w.Published())
			got, err := w.Policy.Allowed(tenancy.WithPrincipal(w.Ctx, tc.principal), w.Tx, tc.grant)
			if err != nil || got != tc.want {
				t.Fatalf("Allowed = %v, %v; want %v", got, err, tc.want)
			}
			if !slices.Equal(before, w.Published()) {
				t.Fatal("a grant read published an event")
			}
		})
	}
}

func currentGrants(t *testing.T, w GrantFixture) {
	ctx := tenancy.WithPrincipal(w.Ctx, tenancy.Principal{UserID: uuid.New(), Roles: []string{"editor"}})
	want := tenancy.Grant{Permission: "role:manage"}
	w.Role("editor", want.Permission)
	if ok, err := w.Policy.Allowed(ctx, w.Tx, want); err != nil || !ok {
		t.Fatalf("initial grant: %v, %v", ok, err)
	}
	w.Role("editor", "widget:read")
	if ok, err := w.Policy.Allowed(ctx, w.Tx, want); err != nil || ok {
		t.Fatalf("revoked grant: %v, %v", ok, err)
	}
}
