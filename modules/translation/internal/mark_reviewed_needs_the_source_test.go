package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// TestAReviewThatCannotSeeTheSourceStampsNothing: a review is a claim that the
// translation still says what the source says. A field whose current source
// the caller did not hand over cannot be checked, so it is not stamped.
func TestAReviewThatCannotSeeTheSourceStampsNothing(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	source := translationtest.NewStubSource()
	machine := &translationtest.Machine{Answer: func(context.Context, string, string, string) (string, error) {
		return "Sobre nós.", nil
	}}
	svc := internal.NewService([]rest.TranslationSource{source}, machine)
	id := uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Suggest(ctx, tx, rest.SuggestQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id, From: "en",
			Fields: []string{"title"}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."},
		}); err != nil {
			t.Fatalf("saving the machine draft: %v", err)
		}
		// The source has since become "About the company."; the caller names
		// the field but hands over no source for it.
		reviewErr := svc.Review(ctx, tx, rest.ReviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Fields: []string{"title"}, Source: map[string]string{},
		})
		var reviewed int
		if err := tx.DB().Raw(`SELECT count(*) FROM translations WHERE record_id = ? AND reviewed_at IS NOT NULL`, id).
			Scan(&reviewed).Error; err != nil {
			t.Fatalf("counting reviewed rows: %v", err)
		}
		if reviewed != 0 {
			t.Errorf("a review with no source to check against stamped %d rows (err %v)", reviewed, reviewErr)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
