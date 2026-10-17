package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

// TestMineAnswersOnlyTheCallersOwnChoices: Mine is "the person's own rows", and
// the person is the credential the request carries. Another signed-in member of
// the same tenant handing the owner's id gets none of the owner's choices back —
// either a policy denial or an empty page, never the rows.
func TestMineAnswersOnlyTheCallersOwnChoices(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	prefs := notification.Settings()
	owner := notificationtest.Ada
	ownCtx := tenancy.WithPrincipal(asAdmin(tenancy.WithTenant(t.Context(), acme)), tenancy.Principal{UserID: owner})
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := prefs.SetChannel(ctx, tx, owner, "", contracts.ChannelEmail, false)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The owner reads their own row: the reachability of the read does not
	// depend on what the other caller is answered.
	if err := db.Run(ownCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, _, err := prefs.Mine(ctx, tx, owner, crud.Query{})
		if err == nil && len(rows) != 1 {
			t.Errorf("owner's own Mine returned %d rows, want 1", len(rows))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other := uuid.New()
	otherCtx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), other), tenancy.Principal{UserID: other})
	err := db.Run(otherCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, total, err := prefs.Mine(ctx, tx, owner, crud.Query{})
		if err != nil {
			return err
		}
		if len(rows) != 0 || total != 0 {
			t.Errorf("another member read %d of the owner's choices (total %d), want none", len(rows), total)
		}
		return nil
	})
	if err != nil && !errors.Is(err, tenancy.ErrPolicyDenied) {
		t.Fatalf("another member's Mine: got %v, want a policy denial or no rows", err)
	}
}
