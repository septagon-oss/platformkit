package main

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestAPasskeySignInGrantDoesNotHoldTheAdministratorsFloor: passkey:signin is the
// second permission the auth module declares, and it opens a door, not the roles
// screen. With bob deactivated, ada is the one active administrator; a role that
// grants only passkey:signin, held by ada, answers for nothing, so rewriting the
// admin role to grant passkey:signin instead of role:manage is refused and ada can
// still administer. The first write proves the catalogue accepts passkey:signin,
// so the refusal is the floor's and not an unknown permission's.
func TestAPasskeySignInGrantDoesNotHoldTheAdministratorsFloor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	catalogue := []tenancy.Grant{
		{Permission: authcontracts.PermissionRoleManage},
		{Permission: authcontracts.PermissionPasskeySignIn},
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Deactivate(ctx, tx, s.bob.ID)
		return err
	}); err != nil {
		t.Fatalf("deactivating bob: %v", err)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, "door", []string{authcontracts.PermissionPasskeySignIn}, catalogue)
		return err
	}); err != nil {
		t.Fatalf("granting passkey:signin to door: %v", err)
	}
	appoint(t, s, s.ada.ID, authcontracts.RoleAdmin, "door")

	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin,
			[]string{authcontracts.PermissionPasskeySignIn}, catalogue)
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("trading admin's role:manage for passkey:signin = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != s.ada.Email {
		t.Errorf("%v can administer after the refused write, want ada", got)
	}
}
