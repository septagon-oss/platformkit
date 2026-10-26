package internal_test

import (
	"context"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"testing"
)

type provisioningOnly struct{ usercontracts.Provisioning }

func TestProvisioningCapabilityCarriesTheAccountLifecycle(t *testing.T) {
	w := compose(t, "member")
	svc, _ := scim.New(scim.Deps{Provisioning: provisioningOnly{w.users}, Users: usertest.NewFake(),
		Roles: roleBook{known: []string{"member"}}, Authority: w.authority})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		draft := contracts.UserDraft{Schemas: []string{contracts.SchemaUser}, UserName: "narrow@acme.test", Active: pointer(true)}
		person, err := svc.CreateUser(ctx, tx, draft)
		if err != nil {
			return err
		}
		if !person.Active {
			t.Fatal("confirmed account is inactive")
		}
		draft.Active = pointer(false)
		person, err = svc.ReplaceUser(ctx, tx, person.ID, draft)
		if err != nil {
			return err
		}
		if person.Active {
			t.Fatal("deactivated account is active")
		}
		draft.Active = pointer(true)
		person, err = svc.ReplaceUser(ctx, tx, person.ID, draft)
		if err != nil {
			return err
		}
		if !person.Active {
			t.Fatal("reactivated account is inactive")
		}
		group, err := svc.CreateGroup(ctx, tx, contracts.GroupDraft{Schemas: []string{contracts.SchemaGroup}, DisplayName: "Members", Members: []contracts.MemberRef{{Value: person.ID.String()}}})
		if err != nil {
			return err
		}
		if _, err = svc.MapGroup(mayMap(ctx), tx, group.DisplayName, "member"); err != nil {
			return err
		}
		account, err := w.users.Get(ctx, tx, person.ID)
		if err == nil && (len(account.Roles) != 1 || account.Roles[0] != "member") {
			t.Fatalf("roles = %v", account.Roles)
		}
		return err
	})
}
