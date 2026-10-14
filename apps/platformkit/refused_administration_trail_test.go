package main

// A write the last-administrator rule refuses is an attempt somebody made on the
// tenant's administration, and the brief asks for its audit record: the refused
// write rolls back, so the record has to be published outside it, the way
// modules/auth records a failed login.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// outboxRows counts this tenant's outbox rows whose payload mentions needle, read
// on the migration connection so the count is the table's and not a policy's.
func outboxRows(t *testing.T, s administrationSessions, needle string) int {
	t.Helper()
	var n int
	if err := s.admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND payload::text LIKE '%' || $2 || '%'`,
		s.tenant.ID, needle).Scan(&n); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	return n
}

// TestARefusedAdministrationWriteLeavesAnAuditRecord runs one refusal at each
// door — the user module's roles edit and the auth module's grant edit — and
// asks that each leaves one row on the trail naming what it tried to move, while
// the domain write itself stays rolled back.
func TestARefusedAdministrationWriteLeavesAnAuditRecord(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	// One administrator left, so the next write at either door takes the last one.
	if err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Deactivate(ctx, tx, s.bob.ID)
		return err
	}); err != nil {
		t.Fatalf("deactivating bob: %v", err)
	}

	subject := s.ada.ID.String()
	was := outboxRows(t, s, subject)
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("the last administrator standing down = %v, want it refused", err)
	}
	if got := outboxRows(t, s, subject); got <= was {
		t.Errorf("the refused roles edit of %s left %d trail rows naming the person, had %d: a refused"+
			" attempt on the tenant's administration must be recorded", subject, got, was)
	}

	role := `"` + authcontracts.RoleAdmin + `"`
	was = outboxRows(t, s, role)
	err = db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin, nil, declared)
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("emptying the last administering role = %v, want it refused", err)
	}
	if got := outboxRows(t, s, role); got <= was {
		t.Errorf("the refused grant edit of role %s left %d trail rows naming the role, had %d: a"+
			" refused attempt on the tenant's administration must be recorded", role, got, was)
	}

	if got := administeringHolders(t, s); len(got) != 1 {
		t.Errorf("%v can administer this tenant after two refused writes, want ada alone", got)
	}
}
