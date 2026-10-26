package main

// A soft-deleted person is still held: their row keeps the address and the name. An erasure
// asked about them runs every composed owner, not a stub, and leaves neither in users.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestErasingASoftDeletedPersonRunsEveryOwnerAndLeavesNoAddress(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service")
	}
	acme := acmeTenant(t, cfg)
	email := "soft.deleted@acme.localhost"
	officerEmail := "soft.officer@acme.localhost"
	provisionAs(t, cfg, c, acme, email, "member")
	provisionAs(t, cfg, c, acme, officerEmail, "admin")

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	var person, officer uuid.UUID
	var name string
	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Raw(`SELECT id, display_name FROM users WHERE email = ?`, email).Row().Scan(&person, &name); err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT id FROM users WHERE email = ?`, officerEmail).Row().Scan(&officer); err != nil {
			return err
		}
		if err := tx.DB().Exec(`UPDATE users SET deleted_at = now() WHERE id = ?`, person).Error; err != nil {
			return err
		}
		var e error
		tenant, e = c.testTenants().ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("seed a soft-deleted person: %v", err)
	}
	in := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), officer)
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, _, err := svc.RequestErase(ctx, tx, subjectcontracts.EraseInput{Subject: person, Confirm: email})
		return err
	}); err != nil {
		t.Fatalf("erasure of a held soft-deleted person through every composed owner: %v", err)
	}

	var rows, named int
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*), count(*) FILTER (WHERE lower(email) = lower(?) OR (? <> '' AND display_name = ?))
			FROM users WHERE id = ?`, email, name, name, person).Row().Scan(&rows, &named)
	}); err != nil {
		t.Fatalf("re-read the person's row: %v", err)
	}
	if rows != 1 {
		t.Fatalf("the person's row is gone (%d rows); erasure anonymises it and keeps the id", rows)
	}
	if named != 0 {
		t.Errorf("users still carries %s or their name after the erasure", email)
	}
}
