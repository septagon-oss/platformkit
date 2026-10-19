package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestConnectorReactivationDoesNotReopenALaterTenantDeactivation(t *testing.T) {
	w := compose(t)
	var person uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "returning@acme.test", ExternalID: "directory-id", Active: new(true)})
		if err != nil {
			return err
		}
		person = u.ID
		return nil
	})
	ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), acme), tenancy.Principal{
		UserID: uuid.New(), Permissions: []string{contracts.PermissionProvision},
	})
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return w.svc.DeactivateUser(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.PatchUser(ctx, tx, person, []contracts.Operation{{Op: "replace", Path: "active", Value: []byte(`true`)}})
		if err == nil && (!u.Active || u.ID != person || u.ExternalID != "directory-id") {
			t.Error("connector did not restore the same directory identity")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var before int
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.users.Deactivate(ctx, tx, person)
		before = len(outbox(t, tx))
		return err
	})
	if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return w.svc.DeactivateUser(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	var returned *contracts.UserResource
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		returned, err = w.svc.PatchUser(ctx, tx, person, []contracts.Operation{{Op: "replace", Path: "active", Value: []byte(`true`)}})
		return err
	})
	if !errors.Is(err, crud.ErrConflict) || returned != nil {
		t.Errorf("tenant closure must survive connector retry: row=%+v error=%v", returned, err)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.GetUser(ctx, tx, person)
		if err == nil && (u.Active || u.ExternalID != "directory-id" || len(outbox(t, tx)) != before) {
			t.Error("refused reactivation or repeated closure changed the person or outbox")
		}
		return err
	})
}
