package main

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The status door's half of the pair passkey_grant_removal_test.go pins at the
// roles door: refuse the write that takes the last administrator away, never the
// write that finds none. Deactivating a person who never administered the tenant
// must succeed even when nobody administers it today — a floor that refused it
// would hold every member hostage to a state none of their writes can repair.
func TestDeactivatingSomebodyWhoNeverAdministeredDoesNotRefuseForFindingNoAdministrator(t *testing.T) {
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
	// contract case, not a claim that this principal can reach the status route.
	appoint(t, s, s.ada.ID, "door")
	appoint(t, s, s.bob.ID, "door")
	if got := administeringHolders(t, s); len(got) != 0 {
		t.Fatalf("fixture has administering holders: %v", got)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := s.c.users.Deactivate(ctx, tx, s.bob.ID)
		if err == nil && (u == nil || u.Status != usercontracts.StatusInactive) {
			t.Errorf("deactivation returned %v, want the row inactive", u)
		}
		return err
	}); err != nil {
		t.Fatalf("deactivating a non-administrator in a tenant already without administrators: %v", err)
	}
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := s.c.users.Get(ctx, tx, s.bob.ID)
		if err != nil {
			return err
		}
		if u.Status != usercontracts.StatusInactive {
			t.Errorf("persisted status = %q, want %q", u.Status, usercontracts.StatusInactive)
		}
		var refusals int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ?", usercontracts.EventAdministrationRefused).Count(&refusals).Error; err != nil {
			return err
		}
		if refusals != 0 {
			t.Errorf("administration refusals = %d, want zero", refusals)
		}
		var deactivations int64
		if err := tx.DB().WithContext(ctx).Table("platformkit_outbox").Where(
			"name = ? AND payload->>'userId' = ?", usercontracts.EventDeactivated, s.bob.ID.String()).Count(&deactivations).Error; err != nil {
			return err
		}
		if deactivations != 1 {
			t.Errorf("deactivation events for this person = %d, want one", deactivations)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
