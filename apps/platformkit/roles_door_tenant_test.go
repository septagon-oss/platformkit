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

// TestAnotherTenantsHoldersDoNotAnswerForTheRolesDoor is the tenant boundary of
// the roles side of the composed check: Globex has an active holder of a role
// that grants role:manage, Acme has a spare granting role held by nobody and one
// active administrator. Emptying Acme's admin role is refused — Globex's person
// and Acme's unheld spare answer for nothing — and Globex's own roles are
// untouched by the attempt.
func TestAnotherTenantsHoldersDoNotAnswerForTheRolesDoor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Deactivate(ctx, tx, s.bob.ID)
		return err
	}); err != nil {
		t.Fatalf("deactivating bob: %v", err)
	}
	makeGrant(t, s, "spare")
	globex := secondTenant(t, cfg, "globex", "root@globex.localhost")
	// Globex holds "spare" too, with an active person in it.
	err := db.Run(tenancy.WithTenant(s.ctx(t), globex), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, "spare", []string{authcontracts.PermissionRoleManage}, declared)
		return err
	})
	if err != nil {
		t.Fatalf("granting spare in globex: %v", err)
	}
	if _, err := s.admin.ExecContext(t.Context(),
		`UPDATE users SET roles = '{admin,spare}' WHERE tenant_id = $1 AND email = 'root@globex.localhost'`,
		globex.ID); err != nil {
		t.Fatalf("appointing globex's administrator to spare: %v", err)
	}

	err = db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin, nil, declared)
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("emptying Acme's admin role = %v, want it refused: another tenant's holders must not answer for this one", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != s.ada.Email {
		t.Errorf("%v can administer Acme after the refused write, want ada", got)
	}
}
