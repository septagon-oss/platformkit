package internal_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestGroupAuthorityRefusalLeavesACommittedTransactionUnchanged(t *testing.T) {
	w := compose(t, "finance")
	var person *contracts.UserResource
	var group *contracts.GroupResource
	var beforeEvents []string
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		person, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "member@acme.test", Active: new(true)})
		if err != nil {
			return err
		}
		group, err = w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Finance", ExternalID: "directory-finance"})
		if err != nil {
			return err
		}
		if _, err := w.svc.MapGroup(mayMap(ctx), tx, "Finance", "finance"); err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &beforeEvents).Error
	})
	ops := []contracts.Operation{{Op: "add", Path: "members", Value: []byte(`[{"value":"` + person.ID.String() + `"}]`)}}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID, Permissions: []string{"user:read"}})
		returned, err := w.svc.PatchGroup(ctx, tx, group.ID, ops)
		if !errors.Is(err, tenancy.ErrPolicyDenied) || returned != nil {
			t.Errorf("read-only membership write: row=%+v error=%v; want refusal and no row", returned, err)
		}
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		stored, err := w.svc.GetGroup(ctx, tx, group.ID)
		if err != nil {
			return err
		}
		if len(stored.Members) != 0 || stored.ExternalID != group.ExternalID || stored.Meta != group.Meta {
			t.Errorf("committed refusal changed group: before=%+v after=%+v", group, stored)
		}
		u, err := w.users.Get(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		var afterEvents []string
		if err := tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &afterEvents).Error; err != nil {
			return err
		}
		if len(u.Roles) != 0 || !slices.Equal(beforeEvents, afterEvents) {
			t.Errorf("committed refusal granted roles or changed events: roles=%v events=%v -> %v", u.Roles, beforeEvents, afterEvents)
		}
		ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person.ID, Permissions: []string{contracts.PermissionProvision}})
		accepted, err := w.svc.PatchGroup(ctx, tx, group.ID, ops)
		if err != nil {
			return err
		}
		if len(accepted.Members) != 1 || accepted.Members[0].Value != person.ID.String() {
			t.Errorf("authorized membership write did not add the person: %+v", accepted)
		}
		u, err = w.users.Get(ctx, tx, person.ID)
		if err == nil && !slices.Contains(u.Roles, "finance") {
			t.Errorf("authorized membership write did not grant mapped role: %v", u.Roles)
		}
		return err
	})
}
