package main

// subject_requests.reason is declared as text a scrub runs over (modules/subject's
// personaldata.Table): it is the person's own words, and a person who wrote their address
// into it is named by it. After their erasure the row stays — it is the receipt — and the
// address does not.

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

func TestErasureScrubsTheExportRequestsOwnWords(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service")
	}
	acme := acmeTenant(t, cfg)
	email := "own.words@acme.localhost"
	provisionAs(t, cfg, c, acme, email, "member")

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	var person uuid.UUID
	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Raw(`SELECT id FROM users WHERE email = ?`, email).Row().Scan(&person); err != nil {
			return err
		}
		var e error
		tenant, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("which person and tenant: %v", err)
	}
	in := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)

	var request uuid.UUID
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.RequestExport(ctx, tx, subjectcontracts.RequestExportInput{
			Subject: person, Reason: "please write to " + email + " when it is ready"})
		if err == nil {
			request = row.ID
		}
		return err
	}); err != nil {
		t.Fatalf("request an export: %v", err)
	}
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, _, err := svc.RequestErase(ctx, tx, subjectcontracts.EraseInput{Subject: person, Confirm: email})
		return err
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var kept, named int
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*), count(*) FILTER (WHERE strpos(lower(reason), ?) > 0 OR strpos(lower(detail), ?) > 0)
			FROM subject_requests WHERE id = ?`, email, email, request).Row().Scan(&kept, &named)
	}); err != nil {
		t.Fatalf("re-read the request row: %v", err)
	}
	if kept != 1 {
		t.Fatalf("the export request row is gone (%d rows); it is the receipt and stays", kept)
	}
	if named != 0 {
		t.Errorf("the export request's reason still carries %s after the erasure", email)
	}
}
