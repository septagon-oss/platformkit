package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

type unavailableRoleDirectory struct{ err error }

func (d unavailableRoleDirectory) HasRole(context.Context, db.Tx[db.Tenant], string) (bool, error) {
	return false, d.err
}

func TestRoleDirectoryFailureDoesNotCommitAMapping(t *testing.T) {
	w := compose(t)
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People"})
		before = len(outbox(t, tx))
		return err
	})
	unavailable := errors.New("role directory unavailable")
	service, _ := scim.New(scim.Deps{Users: w.users, Roles: unavailableRoleDirectory{err: unavailable}})
	var got *contracts.GroupMapping
	err := db.Run(tenancy.WithTenant(t.Context(), acme), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = service.MapGroup(ctx, tx, "People", "finance")
		return err
	})
	if !errors.Is(err, unavailable) || got != nil {
		t.Fatalf("provider failure: mapping=%+v error=%v", got, err)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, err := w.svc.GroupMappings(ctx, tx)
		if err != nil {
			return err
		}
		if len(rows) != 0 || len(outbox(t, tx)) != before {
			t.Error("provider failure committed a mapping or an event")
		}
		return nil
	})
}
