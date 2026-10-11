package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

func TestReviewAndSourceInvalidationDescribeTheStatusTheyReplaced(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Sobre nós."}, Source: map[string]string{"title": "About us."},
			Origin: rest.OriginMachine,
		}); err != nil {
			return err
		}
		query := rest.ReviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Source: map[string]string{"title": "About us."}, Expected: map[string]int64{"title": 1},
		}
		if err := svc.Review(ctx, tx, query); err != nil {
			return err
		}
		reviewed := lastEvent(t, ctx, tx)
		if status := halves(reviewed.Changes, "status"); status != [2]string{`"machine"`, `"complete"`} {
			t.Errorf("review status diff = %v, want machine to complete", status)
		}
		if stamp := halves(reviewed.Changes, "reviewedAt"); stamp[0] != "" || stamp[1] == "" || stamp[1] == "null" {
			t.Errorf("review stamp diff = %v, want absent to a timestamp", stamp)
		}
		if value := halves(reviewed.Changes, "value"); value != [2]string{} {
			t.Errorf("review changed text: %v", value)
		}
		if err := svc.MarkOutdated(ctx, tx, "pages", "page", "title", id); err != nil {
			return err
		}
		outdated := lastEvent(t, ctx, tx)
		if status := halves(outdated.Changes, "status"); status != [2]string{`"complete"`, `"outdated"`} {
			t.Errorf("source invalidation status diff = %v, want complete to outdated", status)
		}
		before := len(publishedPayloads(ctx, tx))
		if err := svc.Review(ctx, tx, query); !errors.Is(err, crud.ErrConflict) {
			t.Fatalf("stale review = %v, want revision conflict", err)
		}
		if after := len(publishedPayloads(ctx, tx)); after != before {
			t.Errorf("refused review emitted %d events", after-before)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
