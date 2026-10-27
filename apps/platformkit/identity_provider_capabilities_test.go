package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/file"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
)

type identityProvisioning struct{ usercontracts.Provisioning }
type identityHostLookup struct{ tenantcontracts.HostLookup }
type identityAuthorizer struct{ calls *int }

func (a identityAuthorizer) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	*a.calls++
	return true, nil
}
func identityProvider[T any](name string, value T) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[T](w, value)
		return module.Module{Name: name}, nil
	}, pkit.Provides[T]())
}

func TestSCIMAndMediaProvidersBuildWithoutIdentityAggregates(t *testing.T) {
	users, calls := usertest.NewFake(), 0
	authn := pkit.Authenticator(func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{}, false, nil
	})
	plan, err := pkit.NewApp("adapters").Use(scimProvisioning, media(),
		identityProvider[httpx.TenantLoader]("hosts", identityHostLookup{tenanttest.NewFake()}),
		identityProvider[pkit.Authenticator]("authenticate", authn),
		identityProvider[httpx.Authorizer]("authorize", identityAuthorizer{&calls}),
		identityProvider[usercontracts.Provisioning]("accounts", identityProvisioning{users}),
		identityProvider[usercontracts.Lookup]("people", identityLookup{users}),
		identityProvider[authcontracts.RoleReader]("roles", identityRoleReader(func(context.Context, db.Tx[db.Tenant]) ([]*authcontracts.Role, error) { return nil, nil })),
		identityProvider[authcontracts.PermissionReader]("permissions", identityPermissions(func(context.Context, db.Tx[db.Tenant], []string) ([]string, error) { return nil, nil })),
	).Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pkit.Value[usercontracts.Service](plan); ok {
		t.Fatal("provider acquired User Service")
	}
	if _, ok := pkit.Value[authcontracts.Auth](plan); ok {
		t.Fatal("provider acquired Auth")
	}
	if _, ok := pkit.Value[tenantcontracts.Service](plan); ok {
		t.Fatal("provider acquired Tenant Service")
	}
	svc, ok := pkit.Value[scimcontracts.Service](plan)
	if !ok {
		t.Fatal("SCIM missing")
	}
	if cfg, err := svc.ProviderConfig(t.Context()); err != nil || cfg == nil {
		t.Fatalf("SCIM config = %v,%v", cfg, err)
	}
	chrome, ok := pkit.Value[*file.LibraryChrome](plan)
	if !ok {
		t.Fatal("media chrome missing")
	}
	if yes, err := chrome.Authorize.Allowed(t.Context(), tenancy.Tenant{}, tenancy.Grant{Permission: "file:manage"}); err != nil || !yes || calls != 1 {
		t.Fatalf("media authorization = %v,%v; calls=%d", yes, err, calls)
	}
}

func TestAccessProviderNeedsOnlyHoldersGrantingAndAuthorization(t *testing.T) {
	fake, calls := usertest.NewFake(), 0
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	person, err := fake.Invite(ctx, tx, "member@example.test", "Member")
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.SetPassword(ctx, tx, person.ID, "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.SetRoles(ctx, tx, person.ID, []string{"member"}); err != nil {
		t.Fatal(err)
	}
	sites := sitetest.NewFake()
	ask := &accessReach{}
	plan, err := pkit.NewApp("access").Use(access(ask),
		identityProvider[httpx.TenantLoader]("hosts", identityHostLookup{tenanttest.NewFake()}),
		identityProvider[pkit.Authenticator]("authenticate", pkit.Authenticator(func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		})),
		identityProvider[httpx.Authorizer]("authorize", identityAuthorizer{&calls}),
		identityProvider[usercontracts.RoleHolders]("people", identityHolders{fake}),
		identityProvider[usercontracts.Administration]("administration", &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return []string{"member"}, nil }}),
		identityProvider[usercontracts.Granting]("granting", &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) { return true, nil }}),
		identityProvider[notificationcontracts.Service]("notices", notificationtest.NewFake()),
		identityProvider[sitecontracts.Service]("sites", sites),
		identityProvider[sitecontracts.LockedReader]("lockedsite", sites),
	).Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := pkit.Value[usercontracts.Service](plan); ok {
		t.Fatal("access acquired User Service")
	}
	if _, ok := pkit.Value[authcontracts.Auth](plan); ok {
		t.Fatal("access acquired Auth")
	}
	recipients, err := ask.Recipients(ctx, tx)
	if err != nil || len(recipients) != 1 || recipients[0] != person.ID {
		t.Fatalf("access recipients = %v,%v", recipients, err)
	}
	if may, err := ask.may(ctx, tx); err != nil || !may {
		t.Fatalf("access granting = %v,%v", may, err)
	}
	chrome, ok := pkit.Value[*change.ReviewChrome](plan)
	if !ok {
		t.Fatal("change chrome missing")
	}
	if yes, err := chrome.Authorize.Allowed(ctx, tenancy.Tenant{}, tenancy.Grant{Permission: "change:review"}); err != nil || !yes || calls != 1 {
		t.Fatalf("change authorization = %v,%v; calls=%d", yes, err, calls)
	}
}
