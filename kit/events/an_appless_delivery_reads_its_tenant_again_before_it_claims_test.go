package events

// The half of the transition the claim trigger's two locks cannot reach: the read
// an app-less delivery makes of its tenant, which is the one fact about the delivery
// the placement changes underneath it.
//
// `Consume` asks `tenants.app` before it opens the transaction that runs the handler,
// and a read taken outside a write is a snapshot the write does not hold: the
// placement can name the tenant between the read and the claim, and the claim then
// marks work done under a durable the tenant's own app will never subscribe under.
// The cure is to ask again inside the transaction, beside the claim, which is what
// `holdsUnscoped` is. This holds the two things that make it the cure rather than a
// second reading of the same column: that it is a *fresh* statement inside an already
// open transaction, so it sees a placement that committed after the transaction began,
// and that it is a read a tenant session is allowed to make at all — row-level
// security shows a tenant its own row and nothing else, and a re-read that answered
// `''` because the policy hid the row would agree with the stale one it replaced.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnApplessDeliveryReadsItsTenantAgainInsideTheClaimTransaction(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	if _, err := owner.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', '')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	// The read every delivery makes first, in its own transaction, outside the one it
	// is going to write its claim in.
	first, err := holdsTenant(ctx, conn, "", tenant.ID)
	if err != nil || !first {
		t.Fatalf("the reading an app-less delivery makes first: holds=%v err=%v, want a tenant nobody named", first, err)
	}
	err = db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before, err := holdsUnscoped(ctx, tx, tenant.ID)
		if err != nil {
			return err
		}
		if !before {
			return errors.New("the delivery's own transaction refused a tenant no placement had named")
		}
		// The placement commits from another session while this transaction is open,
		// which is the moment the reading above is already stale.
		if _, err := owner.ExecContext(ctx,
			`SELECT platformkit_place_tenants('collect', '', 'shop=collect', NULL)`); err != nil {
			return err
		}
		after, err := holdsUnscoped(ctx, tx, tenant.ID)
		if err != nil {
			return err
		}
		if after {
			return errors.New("the re-read inside the open transaction still said the tenant was nobody's " +
				"after a placement named it collect; the claim would be written under a durable that app will never read")
		}
		// What the refusal promises: this delivery wrote nothing on its way out.
		var claims int64
		if err := tx.DB().Raw(`SELECT count(*)::bigint FROM `+handled+` WHERE tenant_id = ?`, tenant.ID).
			Scan(&claims).Error; err != nil {
			return err
		}
		if claims != 0 {
			return errors.New("the refused delivery left a claim behind")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The app-less consumer is not the one that runs this tenant any more, and the
	// reading outside a transaction says so from here on — the re-read is the same
	// answer, taken where it cannot be overtaken.
	outer, err := holdsTenant(ctx, conn, "", tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if outer {
		t.Error("an app-less delivery still holds a tenant a placement named collect")
	}
	var app string
	if err := owner.QueryRowContext(ctx, `SELECT app FROM tenants WHERE id=$1`, tenant.ID).Scan(&app); err != nil {
		t.Fatal(err)
	}
	if app != "collect" {
		t.Errorf("placement left app=%q, want collect", app)
	}
}
