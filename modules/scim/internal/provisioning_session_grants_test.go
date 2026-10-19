package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestGroupCommandsRefuseASessionClaimingRolesItDoesNotHold(t *testing.T) {
	w := compose(t, "finance")
	w.svc, _ = scim.New(scim.Deps{
		Users: w.users, Roles: roleBook{known: []string{"finance"}},
		Authority: grantBook{users: w.users, grants: map[string][]string{administered: {contracts.PermissionProvision}}},
	})
	var person, group uuid.UUID
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "member@acme.test", Active: new(true)})
		if err != nil {
			return err
		}
		person = u.ID
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Finance", ExternalID: "original"})
		if err != nil {
			return err
		}
		group = g.ID
		if _, err := w.svc.MapGroup(mayMap(ctx), tx, "Finance", "finance"); err != nil {
			return err
		}
		before = len(outbox(t, tx))
		return nil
	})
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{
		UserID: person, Roles: []string{administered},
	})
	for _, command := range []string{"create", "replace", "patch", "delete"} {
		t.Run(command, func(t *testing.T) {
			var returned *contracts.GroupResource
			err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				switch command {
				case "create":
					returned, err = w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Unapproved"})
				case "replace":
					returned, err = w.svc.ReplaceGroup(ctx, tx, group, contracts.GroupDraft{DisplayName: "Finance", ExternalID: "unapproved"})
				case "patch":
					returned, err = w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{Op: "add", Path: "members", Value: []byte(`[{"value":"` + person.String() + `"}]`)}})
				case "delete":
					err = w.svc.DeleteGroup(ctx, tx, group)
				}
				return err
			})
			if !errors.Is(err, tenancy.ErrPolicyDenied) || returned != nil {
				t.Errorf("session without live provisioning grants: row=%+v error=%v", returned, err)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				g, err := w.svc.GetGroup(ctx, tx, group)
				if err != nil {
					return err
				}
				groups, err := w.svc.ListGroups(ctx, tx, contracts.ListQuery{})
				if err != nil {
					return err
				}
				u, err := w.users.Get(ctx, tx, person)
				if err != nil {
					return err
				}
				if groups.TotalResults != 1 || g.ExternalID != "original" || len(g.Members) != 0 || len(u.Roles) != 0 || len(outbox(t, tx)) != before {
					t.Error("refused session command changed groups, membership, roles or outbox")
				}
				return nil
			})
		})
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.users.SetRoles(ctx, tx, person, []string{administered})
		return err
	})
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Approved"})
		return err
	}); err != nil {
		t.Fatalf("session with the live provisioning grant was refused: %v", err)
	}
}
