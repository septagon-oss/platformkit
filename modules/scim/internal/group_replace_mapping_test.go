package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestReplacingAGroupPreservesTheTenantRoleMapping(t *testing.T) {
	w := compose(t, "finance")
	var id uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "Finance"})
		if err != nil {
			return err
		}
		id = g.ID
		_, err = w.svc.MapGroup(mayMap(ctx), tx, "Finance", "finance")
		return err
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.ReplaceGroup(ctx, tx, id, contracts.GroupDraft{Schemas: []string{contracts.SchemaGroup}, DisplayName: "Finance", Members: []contracts.MemberRef{}})
		return err
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		g, err := w.svc.GetGroup(ctx, tx, id)
		if err != nil {
			return err
		}
		if g.Role != "finance" {
			t.Errorf("ordinary provider PUT changed tenant mapping to %q; want finance", g.Role)
		}
		return nil
	})
}
