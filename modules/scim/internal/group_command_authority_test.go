package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestGroupCommandsRefuseAnActorWithoutProvisioningAuthority(t *testing.T) {
	for _, command := range []string{"create", "replace", "patch", "delete"} {
		t.Run(command, func(t *testing.T) {
			w := compose(t, "finance")
			var group, person uuid.UUID
			var before int
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: uuid.New(), Permissions: []string{contracts.PermissionProvision}})
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
				UserID: person, Permissions: []string{"user:read"},
			})
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
			if !errors.Is(err, tenancy.ErrPolicyDenied) {
				t.Errorf("read-only actor must be refused, got %v", err)
			}
			if returned != nil {
				t.Errorf("refused command returned group %s", returned.ID)
			}
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if after := len(outbox(t, tx)); after != before {
					t.Errorf("unauthorized command committed events: %d -> %d", before, after)
				}
				groups, err := w.svc.ListGroups(ctx, tx, contracts.ListQuery{})
				if err != nil {
					return err
				}
				if groups.TotalResults != 1 {
					t.Errorf("unauthorized command changed group count to %d", groups.TotalResults)
				}
				g, err := w.svc.GetGroup(ctx, tx, group)
				if err != nil {
					t.Errorf("original group no longer readable: %v", err)
				} else if g.ExternalID != "original" || len(g.Members) != 0 {
					t.Errorf("unauthorized command changed group: externalId=%q members=%v", g.ExternalID, g.Members)
				}
				u, err := w.users.Get(ctx, tx, person)
				if err != nil {
					return err
				}
				if len(u.Roles) != 0 {
					t.Errorf("read-only actor granted itself roles %v", u.Roles)
				}
				return nil
			})
		})
	}
}
