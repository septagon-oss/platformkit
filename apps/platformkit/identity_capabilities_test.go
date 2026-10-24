package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/pkit"
)

type identityLookup struct{ usercontracts.Lookup }
type identityAddress struct{ usercontracts.AddressLookup }
type identitySeedWriter struct{ usercontracts.SeedWriter }
type identityHolders struct{ usercontracts.RoleHolders }
type identityPermissions func(context.Context, db.Tx[db.Tenant], []string) ([]string, error)

func (f identityPermissions) Permissions(ctx context.Context, tx db.Tx[db.Tenant], roles []string) ([]string, error) {
	return f(ctx, tx, roles)
}

type identityRoleReader func(context.Context, db.Tx[db.Tenant]) ([]*authcontracts.Role, error)

func (f identityRoleReader) Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*authcontracts.Role, error) {
	return f(ctx, tx)
}

func TestSCIMAuthorityReadsOnlyLookupAndPermissions(t *testing.T) {
	fake := usertest.NewFake()
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	person, err := fake.Invite(ctx, tx, "ada@example.test", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.SetPassword(ctx, tx, person.ID, "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.SetRoles(ctx, tx, person.ID, []string{"member"}); err != nil {
		t.Fatal(err)
	}
	reads := 0
	source := identityPermissions(func(_ context.Context, _ db.Tx[db.Tenant], roles []string) ([]string, error) {
		reads++
		if !slices.Equal(roles, []string{"member"}) {
			t.Fatalf("current roles = %v", roles)
		}
		return []string{"user:manage"}, nil
	})
	authority := holderGrants{users: identityLookup{fake}, auth: source}
	actor := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID, Roles: []string{"stale"}})
	if yes, err := authority.Holds(actor, tx, "user:manage"); err != nil || !yes || reads != 1 {
		t.Fatalf("holder = %v, %v; reads=%d", yes, err, reads)
	}
	for _, ceiling := range [][]string{{}, {"unrelated:read"}, {"user:manage"}} {
		limited := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID, Permissions: ceiling})
		yes, err := authority.Holds(limited, tx, "user:manage")
		if err != nil || yes != slices.Contains(ceiling, "user:manage") || reads != 1 {
			t.Fatalf("ceiling %v = %v, %v; reads=%d", ceiling, yes, err, reads)
		}
	}
	if _, err := fake.Deactivate(ctx, tx, person.ID); err != nil {
		t.Fatal(err)
	}
	if yes, err := authority.Holds(actor, tx, "user:manage"); err != nil || yes || reads != 1 {
		t.Fatalf("inactive holder = %v, %v", yes, err)
	}
	missing := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New()})
	if yes, err := authority.Holds(missing, tx, "user:manage"); err != nil || yes {
		t.Fatalf("missing holder = %v, %v", yes, err)
	}
}

func TestSCIMDirectoryReadsOnlyRoleNames(t *testing.T) {
	sentinel := errors.New("roles unavailable")
	for _, failure := range []error{nil, sentinel} {
		directory := roleNames{auth: identityRoleReader(func(context.Context, db.Tx[db.Tenant]) ([]*authcontracts.Role, error) {
			return []*authcontracts.Role{{Name: "Member"}}, failure
		})}
		yes, err := directory.HasRole(t.Context(), db.Tx[db.Tenant]{}, " member ")
		if !errors.Is(err, failure) || yes != (failure == nil) {
			t.Fatalf("directory = %v, %v", yes, err)
		}
	}
}

func TestSeedAndAccessConsumersUseTheirAccountCapabilities(t *testing.T) {
	fake := usertest.NewFake()
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	writer := userSeeder{users: identitySeedWriter{fake}, demoPassword: "a sufficiently long password"}
	target := seed.Target{Fields: map[string]any{"email": "seed@example.test", "displayName": "Seed", "roles": []string{"member"}, "signIn": signInDeclared}}
	created, err := writer.Create(ctx, tx, target)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []seed.Key{{RecordID: created.ID}, {Value: "seed@example.test"}} {
		current, err := writer.Read(ctx, tx, key, false)
		if err != nil || !current.Present || current.ID != created.ID {
			t.Fatalf("seed read = %v, %v", current, err)
		}
		if _, err := writer.Update(ctx, tx, current, target); err != nil {
			t.Fatal(err)
		}
	}
	actor, err := seedActor(ctx, identityAddress{fake}, tx, "seed@example.test")
	if err != nil {
		t.Fatal(err)
	}
	principal, _ := tenancy.PrincipalFrom(actor)
	if principal.UserID != created.ID {
		t.Fatalf("seed actor = %+v", principal)
	}
	reach := accessReach{users: identityHolders{fake}, administration: &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) { return []string{"member"}, nil }}}
	recipients, err := reach.Recipients(ctx, tx)
	if err != nil || !slices.Equal(recipients, []uuid.UUID{created.ID}) {
		t.Fatalf("recipients = %v, %v", recipients, err)
	}
}

func TestReferencePlanConstructsGrantsBeforeUserAndAuth(t *testing.T) {
	c := composeReference(referenceCompositionConfig(t), pkit.Development)
	names := moduleNames(c.modules)
	grants, users, auth := slices.Index(names, "authgrants"), slices.Index(names, "user"), slices.Index(names, "auth")
	if grants < 0 || grants >= users || users >= auth {
		t.Fatalf("identity order = %v", names)
	}
	if c.tenantList == nil || c.hostLookup == nil || c.oidcSettings == nil || c.samlSettings == nil || c.bootstrap == nil || c.locale == nil || c.provisioner == nil || c.addresses == nil || c.seedUsers == nil || c.permissions == nil || c.roleRepair == nil {
		t.Fatal("a narrow app output is missing")
	}
}
