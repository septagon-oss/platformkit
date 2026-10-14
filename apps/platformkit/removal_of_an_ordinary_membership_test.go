package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Removing an ordinary role must remain possible when the tenant already has
// no administrator. The membership door must distinguish finding none from
// taking the last administrator away.
func TestRemovingAnOrdinaryMembershipDoesNotRefuseForFindingNoAdministrator(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	catalogue := []tenancy.Grant{{Permission: authcontracts.PermissionPasskeySignIn}}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := s.c.auth.SetRole(ctx, tx, "door", []string{authcontracts.PermissionPasskeySignIn}, catalogue)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Seed a tenant already without an administering person. This is a service
	// contract case, not a claim that this principal can reach the membership route.
	appoint(t, s, s.ada.ID, "door")
	appoint(t, s, s.bob.ID, "door")
	if got := administeringHolders(t, s); len(got) != 0 {
		t.Fatalf("fixture has administering holders: %v", got)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := s.c.users.SetRoles(ctx, tx, s.bob.ID, nil)
		if err == nil && (u == nil || len(u.Roles) != 0) {
			t.Errorf("membership removal returned %v, want no roles", u)
		}
		return err
	}); err != nil {
		t.Fatalf("removing an ordinary membership in a tenant already without administrators: %v", err)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := s.c.users.Get(ctx, tx, s.bob.ID)
		if err != nil {
			return err
		}
		if len(u.Roles) != 0 {
			t.Errorf("persisted roles = %v, want no roles", u.Roles)
		}
		var refusals int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ?", usercontracts.EventAdministrationRefused).Count(&refusals).Error; err != nil {
			return err
		}
		if refusals != 0 {
			t.Errorf("administration refusals = %d, want zero", refusals)
		}
		var roleChanges int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ? AND payload->>'userId' = ?", usercontracts.EventRolesSet, s.bob.ID.String()).Count(&roleChanges).Error; err != nil {
			return err
		}
		if roleChanges != 1 {
			t.Errorf("roles-set events for this person = %d, want one", roleChanges)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
