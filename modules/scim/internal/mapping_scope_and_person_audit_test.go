package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestAMappingWrittenByACredentialThatCarriesUserManage is the other half of
// TestMappingCommandRefusesAProvisionOnlyActor. The command-level check is a
// ceiling and not a wall: a credential that carries the permission the mapping door
// names must still be able to bind a group, or the recheck would be a rule that
// refuses its own tenant.
func TestAMappingWrittenByACredentialThatCarriesUserManage(t *testing.T) {
	w := compose(t, "finance")
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People"})
		return err
	})
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{
		UserID: uuid.New(), Permissions: []string{usercontracts.PermissionUserManage},
	})
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		m, err := w.svc.MapGroup(ctx, tx, "People", "finance")
		if err != nil {
			return err
		}
		if m == nil || m.Role != "finance" {
			t.Errorf("mapping = %+v", m)
		}
		return nil
	}); err != nil {
		t.Fatalf("a credential that carries user:manage was refused: %v", err)
	}
}

// TestAPersonWriteThatChangedNothingLeavesNoAuditLine is the half of
// TestEveryUserAttributeWriteLeavesATransactionalAuditEvent that stops the cure
// from becoming noise: a connector that re-sent what the person already was
// changed nobody, and an outbox full of retries is an audit trail nobody reads.
func TestAPersonWriteThatChangedNothingLeavesNoAuditLine(t *testing.T) {
	w := compose(t)
	var person uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "person@acme.test", DisplayName: "Ada", ExternalID: "ext-1"})
		person = u.ID
		return err
	})
	before := 0
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before = len(outbox(t, tx))
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.ReplaceUser(ctx, tx, person, contracts.UserDraft{UserName: "person@acme.test", DisplayName: "Ada", ExternalID: "ext-1"})
		return err
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("a replace that restated the person emitted %v", outbox(t, tx)[before:])
		}
		return nil
	})
}
