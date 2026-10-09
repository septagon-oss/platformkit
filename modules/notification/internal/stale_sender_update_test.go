package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func TestAStaleSenderEditPreservesTheCommittedIdentity(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()
	ctx := asAdmin(tenancy.WithTenant(t.Context(), acme))
	var stale contracts.Sender
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := store.Put(ctx, tx, sender())
		if err == nil {
			stale = *row
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		current := stale
		current.FromName = "Updated identity"
		_, err := store.Put(ctx, tx, current)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// Another editor changes only ReplyTo in the snapshot read before the
	// identity change committed. Its stale FromName must not replace that edit.
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before := len(outbox(t, tx))
		stale.ReplyTo = "help@acme.example.com"
		row, err := store.Put(ctx, tx, stale)
		if !errors.Is(err, crud.ErrConflict) || row != nil {
			t.Errorf("stale Put: error=%v, returned row=%v; want conflict and no row", err, row != nil)
		}
		if after := len(outbox(t, tx)); after != before {
			t.Errorf("stale Put published %d events, want zero", after-before)
		}
		saved, readErr := store.For(ctx, tx)
		if readErr != nil {
			return readErr
		}
		if saved.FromName != "Updated identity" || saved.ReplyTo != "" {
			t.Errorf("stale Put replaced committed data: name=%q replyTo=%q", saved.FromName, saved.ReplyTo)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
