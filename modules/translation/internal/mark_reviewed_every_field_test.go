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

// TestReviewWithNoFieldsNamedReviewsEveryField: rest.ReviewQuery says an empty
// Fields is "every field of this record in this locale", so "Mark reviewed" on
// the record stamps every machine draft it holds.
func TestReviewWithNoFieldsNamedReviewsEveryField(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	source := translationtest.NewStubSource()
	machine := &translationtest.Machine{Answer: func(context.Context, string, string, string) (string, error) {
		return "Sobre nós.", nil
	}}
	svc := internal.NewService([]rest.TranslationSource{source}, machine)
	id := uuid.New()
	src := map[string]string{"title": "About us."}

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Suggest(ctx, tx, rest.SuggestQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id, From: "en",
			Fields: []string{"title"}, Expected: map[string]int64{"title": 0}, Source: src,
		}); err != nil {
			t.Fatalf("saving the machine draft: %v", err)
		}
		if err := svc.Review(ctx, tx, rest.ReviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Source: src, RichText: map[string]bool{"title": false},
		}); err != nil {
			t.Errorf("reviewing every field of the record: %v", err)
		}
		var unreviewed int
		if err := tx.DB().Raw(`SELECT count(*) FROM translations WHERE record_id = ? AND reviewed_at IS NULL`, id).
			Scan(&unreviewed).Error; err != nil {
			t.Fatalf("counting unreviewed rows: %v", err)
		}
		if unreviewed != 0 {
			t.Errorf("%d machine drafts are still unreviewed after the record was marked reviewed", unreviewed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
