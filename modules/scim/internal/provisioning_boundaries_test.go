package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestSCIMRefusesTheLastAdministratorWithoutChangingState(t *testing.T) {
	w := compose(t, "admin")
	var id uuid.UUID
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "administrator@acme.test", Active: new(true)})
		if err != nil {
			return err
		}
		id = u.ID
		_, err = w.users.SetRoles(ctx, tx, id, []string{"admin"})
		if err != nil {
			return err
		}
		before = len(outbox(t, tx))
		return nil
	})
	var returned *contracts.UserResource
	err := db.Run(tenancy.WithTenant(t.Context(), acme), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		returned, err = w.svc.PatchUser(ctx, tx, id, []contracts.Operation{{Op: "replace", Path: "active", Value: []byte(`false`)}})
		return err
	})
	if err == nil {
		t.Error("last administrator deactivation was accepted")
	}
	if returned != nil {
		t.Errorf("refusal returned stale person: %+v", returned)
	}
	t.Logf("last administrator refusal: %v", err)
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.GetUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if !u.Active {
			t.Error("refused deactivation changed status")
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("refused deactivation changed outbox: %d -> %d", before, after)
		}
		return nil
	})
}

func TestForeignTenantCannotMutateOrReadProvisionedState(t *testing.T) {
	w := compose(t)
	var group, person uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "member@acme.test", ExternalID: "external", Active: new(true)})
		if err != nil {
			return err
		}
		person = u.ID
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People", Members: []contracts.MemberRef{{Value: person.String()}}})
		if err != nil {
			return err
		}
		group = g.ID
		return nil
	})
	for _, tc := range []struct {
		name string
		call func(context.Context, db.Tx[db.Tenant]) error
	}{
		{"deactivate", func(ctx context.Context, tx db.Tx[db.Tenant]) error { return w.svc.DeactivateUser(ctx, tx, person) }},
		{"membership", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{Op: "remove", Path: "members"}})
			if row != nil {
				t.Error("foreign write returned a group")
			}
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.Run(tenancy.WithTenant(t.Context(), globex), w.conn, tc.call)
			if !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("foreign write: %v, want not found", err)
			}
		})
	}
	w.inTenant(t, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, table := range []string{"scim_groups", "scim_group_members", "scim_group_mappings", "scim_user_external"} {
			var count int64
			if err := tx.DB().Table(table).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				t.Errorf("RLS exposed %d rows from %s", count, table)
			}
		}
		if names := outbox(t, tx); len(names) != 0 {
			t.Errorf("foreign refusal emitted %v", names)
		}
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.GetUser(ctx, tx, person)
		if err != nil {
			return err
		}
		g, err := w.svc.GetGroup(ctx, tx, group)
		if err != nil {
			return err
		}
		if !u.Active || u.ExternalID != "external" || len(g.Members) != 1 {
			t.Errorf("foreign writes changed owner state: user=%+v group=%+v", u, g)
		}
		return nil
	})
}
