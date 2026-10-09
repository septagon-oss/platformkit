package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestMappingCommandRefusesASessionWithoutRoles(t *testing.T) {
	w := compose(t, "finance")
	var actor uuid.UUID
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.users.Invite(ctx, tx, "member@acme.test", "Member")
		if err != nil {
			return err
		}
		u, err = w.users.ConfirmAddress(ctx, tx, u.ID, u.Email)
		if err != nil {
			return err
		}
		actor = u.ID
		if len(u.Roles) != 0 {
			t.Fatalf("fixture member unexpectedly holds roles: %v", u.Roles)
		}
		_, err = w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People"})
		before = len(outbox(t, tx))
		return err
	})
	// A session carries no permission ceiling; its roles decide its authority.
	// This real tenant member has no roles and therefore no user:manage grant.
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{UserID: actor})
	var got *contracts.GroupMapping
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = w.svc.MapGroup(ctx, tx, "People", "finance")
		return err
	})
	if !errors.Is(err, tenancy.ErrPolicyDenied) {
		t.Errorf("session without roles must be denied mapping authority, got %v", err)
	}
	if got != nil {
		t.Errorf("refused mapping command returned a row: %+v", got)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := w.svc.GroupMappings(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Errorf("session without roles committed mappings: %+v", rows)
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("refused mapping emitted events: %d -> %d", before, after)
		}
		return nil
	})
}
