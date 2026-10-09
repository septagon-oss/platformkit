package main

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// TestADeactivationAndAnEmptyingWriteRace is the brief's deactivation half of
// the cross-module race: two active people hold two different roles that each
// grant role:manage; one session empties bob's role while another deactivates
// ada. Whichever goes second is refused, in both orders, and somebody active
// can still administer the tenant.
func TestADeactivationAndAnEmptyingWriteRace(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	appoint(t, s, s.bob.ID, authcontracts.RoleMember, "second")
	makeGrant(t, s, "second")

	emptying := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, "second", nil, declared)
		return err
	}
	deactivating := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Deactivate(ctx, tx, s.ada.ID)
		return err
	}

	if err := oneHoldsTheOtherWaits(t, s, "a deactivation after an emptying", emptying, deactivating); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("deactivating ada once bob's role had been emptied = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != s.ada.Email {
		t.Errorf("%v can administer this tenant, want ada, whose deactivation was refused", got)
	}

	// The other order, from the state the first started from.
	makeGrant(t, s, "second")
	if err := oneHoldsTheOtherWaits(t, s, "an emptying after a deactivation", deactivating, emptying); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("emptying bob's role once ada had been deactivated = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != s.bob.Email {
		t.Errorf("%v can administer this tenant, want bob, whose role's emptying was refused", got)
	}
}
