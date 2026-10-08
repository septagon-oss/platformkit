package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestSenderCommandsRefuseAMemberWithoutTheManageGrant(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()
	var id uuid.UUID
	if err := db.Run(asAdmin(tenancy.WithTenant(t.Context(), acme)), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err == nil {
			id = row.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	member := uuid.New()
	memberCtx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), member), tenancy.Principal{UserID: member})
	for _, command := range []string{"put", "verify", "delete"} {
		t.Run(command, func(t *testing.T) {
			err := db.Run(memberCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				before := len(outbox(t, tx))
				var row *contracts.Sender
				var err error
				switch command {
				case "put":
					in := sender()
					in.FromName = "Changed by an unprivileged member"
					row, err = store.Put(ctx, tx, in)
				case "verify":
					row, err = store.Verify(ctx, tx, id)
				case "delete":
					err = store.Delete(ctx, tx, id)
				}
				if !errors.Is(err, tenancy.ErrPolicyDenied) {
					t.Errorf("%s without sender:manage: got %v, want policy denial", command, err)
				}
				if row != nil {
					t.Error("refused command returned a sender row")
				}
				saved, err := store.For(ctx, tx)
				if err != nil {
					return err
				}
				if saved == nil || saved.FromName != "Acme" || saved.Status != contracts.SenderPending {
					t.Error("unprivileged command changed the tenant sender")
				}
				if after := len(outbox(t, tx)); after != before {
					t.Errorf("unprivileged command published %d events", after-before)
				}
				return errRollback
			})
			if !errors.Is(err, errRollback) {
				t.Fatal(err)
			}
		})
	}
}
