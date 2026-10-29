package main

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestErasureHandlesDistinctProvisioningIdentifiersThatContainTheSameAddress(t *testing.T) {
	const email = "Provisioned.Person@Example.com"
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	plan := composeReference(cfg, pkit.Development).plan()
	subjects, ok := pkit.Value[subjectcontracts.Service](plan)
	if !ok || subjects == nil {
		t.Fatal("the composition hands out no subjectcontracts.Service")
	}
	provisioning, ok := pkit.Value[scimcontracts.Service](plan)
	if !ok || provisioning == nil {
		t.Fatal("the composition hands out no scimcontracts.Service")
	}
	invitations, ok := pkit.Value[usercontracts.InvitationCommands](plan)
	if !ok || invitations == nil {
		t.Fatal("the composition hands out no usercontracts.InvitationCommands")
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	var tenant tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = c.testTenants().ByHost(ctx, tx, acmeHost)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	in := tenancy.WithTenant(t.Context(), tenant)
	var person, colleague *scimcontracts.UserResource
	var acting uuid.UUID
	var before int64
	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		person, err = provisioning.CreateUser(ctx, tx, scimcontracts.UserDraft{
			Schemas: []string{scimcontracts.SchemaUser}, UserName: email,
			DisplayName: "Provisioned Person", ExternalID: email,
		})
		if err != nil {
			return err
		}
		colleague, err = provisioning.CreateUser(ctx, tx, scimcontracts.UserDraft{
			Schemas: []string{scimcontracts.SchemaUser}, UserName: "colleague@example.com",
			DisplayName: "Other Person", ExternalID: strings.ToLower(email),
		})
		if err != nil {
			return err
		}
		if err := tx.DB().Table("platformkit_outbox").Count(&before).Error; err != nil {
			return err
		}
		// The withdrawal below is asked for by the tenant's own administrator, because
		// that command answers for the caller who asks it and not for the transaction.
		var ids []uuid.UUID
		if err := tx.DB().Table("users").Where("email = ? AND deleted_at IS NULL", adminEmail).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) != 1 {
			t.Fatalf("%s names %d people, want the one who may withdraw an invitation", adminEmail, len(ids))
		}
		acting = ids[0]
		return nil
	}); err != nil {
		t.Fatalf("provision two distinct identifiers through the module's commands: %v", err)
	}
	// The withdrawal comes first: a link this tenant has not taken up is withdrawn by
	// its own command at the revision it was last read at, because a generic erasure of
	// a live invitation is the correctable conflict the module names, and an erasure is
	// not a way to skip that withdrawal.
	if err := db.Run(tenancy.WithPrincipal(in, tenancy.Principal{UserID: acting}), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := invitations.RevokeInvitation(ctx, tx, person.ID, 1)
		return err
	}); err != nil {
		t.Fatalf("withdraw the provisioned person's invitation: %v", err)
	}

	var receipt *subjectcontracts.Erasure
	err = db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = subjects.Erase(ctx, tx, subjectcontracts.EraseInput{Subject: person.ID, Confirm: email})
		return err
	})
	if err != nil {
		t.Errorf("an erasure of a provisioned person must handle distinct identifiers containing their address: %v", err)
		if receipt != nil {
			t.Errorf("a refused erasure returned a receipt: %+v", receipt)
		}
	}
	if readErr := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var identifiers []string
		if err := tx.DB().Table("scim_user_external").
			Where("user_id IN ?", []any{person.ID, colleague.ID}).Pluck("external_id", &identifiers).Error; err != nil {
			return err
		}
		for _, identifier := range identifiers {
			if strings.Contains(strings.ToLower(identifier), strings.ToLower(email)) {
				t.Errorf("the provisioning identifier still contains the erased address: %q", identifier)
			}
		}
		other, readErr := provisioning.GetUser(ctx, tx, colleague.ID)
		if readErr != nil {
			return readErr
		}
		if other.UserName != "colleague@example.com" || other.DisplayName != "Other Person" {
			t.Errorf("erasing the mentioned person changed the colleague's identity: %+v", other)
		}
		if err != nil {
			var after int64
			if countErr := tx.DB().Table("platformkit_outbox").Count(&after).Error; countErr != nil {
				return countErr
			}
			if after != before {
				t.Errorf("the refused erasure changed its outbox: before=%d after=%d", before, after)
			}
			t.Logf("refused erasure receipt=%v, outbox before=%d after=%d", receipt, before, after)
		}
		return nil
	}); readErr != nil {
		t.Fatalf("read the committed state: %v", readErr)
	}
}
