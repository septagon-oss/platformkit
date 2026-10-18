package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestMappingCommandRefusesAProvisionOnlyActor(t *testing.T) {
	w := compose(t, "finance")
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People"})
		before = len(outbox(t, tx))
		return err
	})
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{
		UserID: uuid.New(), Permissions: []string{contracts.PermissionProvision},
	})
	var got *contracts.GroupMapping
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = w.svc.MapGroup(ctx, tx, "People", "finance")
		return err
	})
	if err == nil {
		t.Error("MapGroup accepted a provision-only actor without user:manage")
	}
	if got != nil {
		t.Errorf("unauthorized command returned a mapping: %+v", got)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := w.svc.GroupMappings(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 0 {
			t.Errorf("unauthorized command committed %d mapping(s)", len(rows))
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("unauthorized command emitted events: %d -> %d", before, after)
		}
		return nil
	})
}
