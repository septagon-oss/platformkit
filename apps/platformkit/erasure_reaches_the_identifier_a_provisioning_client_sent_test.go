package main

// A provisioning client names a person by an identifier of its own (SCIM's externalId), and
// the scim module keeps it in scim_user_external.external_id as the text the client sent.
// A directory that keys people by mail sends their address. An erasure asked about that
// person, through every composed owner, has to leave no address of theirs in that column
// either: the brief's line is "no email or name of theirs in any table".
//
// It is an application case for the same reason the acceptance line is: no module may read
// another's table, and the composition is the one place the person's row and the
// provisioning client's identifier for it are both in front of one command.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestErasureLeavesNoAddressInTheIdentifierAProvisioningClientSent(t *testing.T) {
	const (
		email  = "Provisioned.Person@Example.com"
		name   = "Provisioned Person"
		handle = "pperson"
	)
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	svc, ok := pkit.Value[subjectcontracts.Service](composeReference(cfg, pkit.Development).plan())
	if !ok || svc == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service")
	}

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		tenant, e = c.testTenants().ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("which tenant is acme: %v", err)
	}
	in := tenancy.WithTenant(t.Context(), tenant)

	person := uuid.New()
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec(`INSERT INTO users (id, tenant_id, email, display_name, handle) VALUES (?, ?, ?, ?, ?)`,
			person, tenant.ID, email, name, handle).Error; err != nil {
			return err
		}
		return tx.DB().Exec(`INSERT INTO scim_user_external (tenant_id, user_id, external_id) VALUES (?, ?, ?)`,
			tenant.ID, person, email).Error
	}); err != nil {
		t.Fatalf("seed a provisioned person: %v", err)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Erase(ctx, tx, subjectcontracts.EraseInput{Subject: person, Confirm: email})
		return err
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var held []string
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT external_id FROM scim_user_external WHERE user_id = ?`, person).Scan(&held).Error
	}); err != nil {
		t.Fatalf("re-read the provisioning identifier: %v", err)
	}
	for _, external := range held {
		if strings.Contains(strings.ToLower(external), strings.ToLower(email)) {
			t.Errorf("scim_user_external still names the person after the erasure: external_id = %q", external)
		}
	}
}
