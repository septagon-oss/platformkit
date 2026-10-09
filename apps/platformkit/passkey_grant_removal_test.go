package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestRemovingAPasskeyGrantDoesNotRefuseForFindingNoAdministrator(t *testing.T) {
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
	// contract case, not a claim that this principal can reach the roles route.
	appoint(t, s, s.ada.ID, "door")
	appoint(t, s, s.bob.ID, "door")
	if got := administeringHolders(t, s); len(got) != 0 {
		t.Fatalf("fixture has administering holders: %v", got)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		role, err := s.c.auth.SetRole(ctx, tx, "door", nil, catalogue)
		if err == nil && (role == nil || len(role.Grants) != 0) {
			t.Errorf("removed passkey grant returned role %v, want an empty grant list", role)
		}
		return err
	}); err != nil {
		t.Fatalf("removing a non-administrative grant from a tenant already without administrators: %v", err)
	}
	if got := roleGrants(t, s, "door"); len(got) != 0 {
		t.Errorf("persisted grants = %v, want empty", got)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var refusals int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ?", authcontracts.EventAdministrationRefused).Count(&refusals).Error; err != nil {
			return err
		}
		if refusals != 0 {
			t.Errorf("administration refusals = %d, want zero", refusals)
		}
		var changes int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ? AND payload->>'role' = ?", authcontracts.EventRoleSet, "door").Count(&changes).Error; err != nil {
			return err
		}
		if changes != 2 {
			t.Errorf("role changes = %d, want grant and removal", changes)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
