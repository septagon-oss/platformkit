package internal_test

// The second half of the core review of 2026-09-29 (P2), ported from
// refs/reviews/pkit-core-2026-09-29/audit_review_test.go's
// TestReviewSiteAuditExplainsATaglineChange. It is here rather than in modules/audit
// because the change it names is this module's payload, and a module's test may not
// import another module's manifest (scripts/check_imports.sh).
//
// The review's case read the trail; this one reads the outbox payload, which is the
// same bytes: modules/audit stores an event's payload verbatim (its Record copies
// events.Event.Payload into a jsonb column and invents nothing), so what this payload
// says is what the trail will say a year from now. Reading it here also says which
// module owns the claim.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/site/internal"
)

// TestReviewSiteAuditExplainsATaglineChange: saving two different taglines used to
// produce two audit events, "neither containing either tagline" (reproductions.log).
// Each event now carries the field it moved, the value it left and the value it took.
func TestReviewSiteAuditExplainsATaglineChange(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	svc := internal.NewService()

	var payloads []string
	for _, tagline := range []string{"Original review tagline", "Replacement review tagline"} {
		line := ""
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.Save(ctx, tx, &contracts.SiteSettings{Title: "Review site", Tagline: tagline}); err != nil {
				return err
			}
			return tx.DB().Raw("SELECT payload::text FROM platformkit_outbox ORDER BY created_at DESC LIMIT 1").Scan(&line).Error
		})
		if err != nil {
			t.Fatalf("save %q: %v", tagline, err)
		}
		payloads = append(payloads, line)
	}
	joined := strings.Join(payloads, " ")
	for _, want := range []string{"Original review tagline", "Replacement review tagline"} {
		if !strings.Contains(joined, want) {
			t.Errorf("neither event names %q, so the trail cannot say what the save changed: %s", want, joined)
		}
	}
	// The diff's names are the payload's own, and the second event says where the
	// first one's value went.
	if !strings.Contains(payloads[1], `"before": "Original review tagline"`) ||
		!strings.Contains(payloads[1], `"after": "Replacement review tagline"`) {
		t.Errorf("the second event is not a change from the first's value: %s", payloads[1])
	}
	if strings.Contains(payloads[0], `"before":`) {
		t.Errorf("the tenant's first save claims a value it replaced: %s", payloads[0])
	}

	var events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Errorf("two saves that changed something wrote %d outbox rows, want two", events)
	}
}

// TestAChangeThatReachesNoFieldPublishesNothing is the other half of the same rule:
// the diff replaced same(), so "nothing changed" is one comparison rather than a list
// of fields that can drift from the columns a save writes.
func TestAChangeThatReachesNoFieldPublishesNothing(t *testing.T) {
	admin, conn := dbtest.Schema(t, site.Migrations)
	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New()})
	svc := internal.NewService()
	in := &contracts.SiteSettings{Title: "Acme", Tagline: "We make things", Theme: "system"}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		saved, err := svc.Save(ctx, tx, in)
		if err != nil {
			return err
		}
		again, err := svc.Save(ctx, tx, saved)
		if err != nil {
			return err
		}
		if again.Revision != 1 {
			t.Errorf("saving the row back unchanged moved its revision to %d", again.Revision)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("a save that moved no field published %d events, want the one the first save caused", events)
	}
}
