package internal_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestWithdrawingAMappingRevokesTheRoleItGranted(t *testing.T) {
	for _, replacement := range []string{"", "editor"} {
		t.Run("replacement="+replacement, func(t *testing.T) {
			w := compose(t, "finance", "editor")
			var id uuid.UUID
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "member@acme.test", Active: new(true)})
				if err != nil {
					return err
				}
				id = u.ID
				if _, err = w.users.SetRoles(ctx, tx, id, []string{"reader"}); err != nil {
					return err
				}
				_, err = w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Finance", Members: []contracts.MemberRef{{Value: id.String()}}})
				if err != nil {
					return err
				}
				_, err = w.svc.MapGroup(ctx, tx, "Finance", "finance")
				return err
			})
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.users.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				if !slices.Contains(u.Roles, "finance") {
					t.Fatal("setup did not grant finance")
				}
				_, err = w.svc.MapGroup(ctx, tx, "Finance", replacement)
				return err
			})
			w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				u, err := w.users.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				if slices.Contains(u.Roles, "finance") {
					t.Errorf("withdrawn mapping left finance assigned: %v", u.Roles)
				}
				if !slices.Contains(u.Roles, "reader") {
					t.Errorf("unmanaged reader role lost: %v", u.Roles)
				}
				if replacement != "" && !slices.Contains(u.Roles, replacement) {
					t.Errorf("replacement role missing: %v", u.Roles)
				}
				return nil
			})
		})
	}
}
