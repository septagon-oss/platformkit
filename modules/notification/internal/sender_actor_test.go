package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// theAuthor is the person the second half of the case names as its caller.
var theAuthor = uuid.MustParse("7c1f0f0a-0000-4000-8000-000000000001")

// senderEvents is what this transaction published about the tenant's sender.
func senderEvents(t *testing.T, tx db.Tx[db.Tenant]) []contracts.SenderSet {
	t.Helper()
	type row struct {
		Name    string
		Payload []byte
	}
	var rows []row
	err := tx.DB().Table("platformkit_outbox").Select("name, payload").
		Where("name IN ?", []string{contracts.EventSenderSet, contracts.EventSenderVerified}).
		Order("created_at, id").Scan(&rows).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	out := make([]contracts.SenderSet, 0, len(rows))
	for _, r := range rows {
		var p contracts.SenderSet
		if err := json.Unmarshal(r.Payload, &p); err != nil {
			t.Fatalf("read the %s payload: %v", r.Name, err)
		}
		out = append(out, p)
	}
	return out
}

// TestTheSenderCommandsRefuseACallerWhoIsNobody pins what contracts.SenderAdmin
// promises — "Both commands recheck the actor's grant and the tenant's own row
// inside their transaction, and a refusal writes nothing, publishes nothing and
// returns no stale row." None of Put, Verify and Delete asks who is calling:
// tenancy.ActorFrom's second result is dropped at both places it is read
// (internal/senders.go), so a call made from anywhere at all — a job, a retry, a
// route composed without a grant check — writes the tenant's mail identity and
// publishes notification.sender_set with actor 00000000-… , which is the audit
// answer to "who set this tenant's sender" being nobody. A command that cannot
// attribute its own write has not rechecked its actor.
//
// The second half is the guard against the cure being "refuse everything": with a
// caller named in the context, the same write goes through and the event names
// that caller.
func TestTheSenderCommandsRefuseACallerWhoIsNobody(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	store := senders()

	// A transaction that names no actor is the caller the module must refuse.
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, ok := tenancy.ActorFrom(ctx); ok {
			t.Fatal("this transaction names an actor; the case is about one that names nobody")
		}
		if row, err := store.Put(ctx, tx, sender()); err == nil {
			t.Errorf("Put by a caller nobody wrote sender %s, want a refusal that writes nothing", row.ID)
		}
		if _, err := store.Verify(ctx, tx, uuid.New()); err == nil {
			t.Error("Verify by a caller nobody succeeded")
		}
		if err := store.Delete(ctx, tx, uuid.New()); err == nil {
			t.Error("Delete by a caller nobody succeeded")
		}
		var live int64
		if err := tx.DB().Raw(`SELECT count(*) FROM notification_senders WHERE deleted_at IS NULL`).Scan(&live).Error; err != nil {
			return err
		}
		if live != 0 {
			t.Errorf("a caller nobody wrote %d sender rows, want none", live)
		}
		if got := senderEvents(t, tx); len(got) != 0 {
			t.Errorf("a caller nobody published %d sender events, want none", len(got))
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		t.Fatalf("sender commands with no actor: %v", err)
	}

	// With a caller named, the same write is the tenant's own and the trail says
	// who did it — which is the assertion that keeps the refusal above from being
	// satisfiable by refusing every call.
	err = db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), theAuthor), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := store.Put(ctx, tx, sender()); err != nil {
				t.Errorf("Put by a named caller: %v", err)
				return errRollback
			}
			got := senderEvents(t, tx)
			if len(got) != 1 {
				t.Errorf("the named caller's write published %d sender events, want 1", len(got))
				return errRollback
			}
			if got[0].Actor != theAuthor {
				t.Errorf("notification.sender_set names actor %s, want %s", got[0].Actor, theAuthor)
			}
			return errRollback
		})
	if err != nil && !errors.Is(err, errRollback) {
		t.Fatalf("sender commands with a named caller: %v", err)
	}
}
