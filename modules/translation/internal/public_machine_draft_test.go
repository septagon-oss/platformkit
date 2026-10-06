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

// TestAnOutdatedMachineDraftIsNeverServedPublicly: a machine draft nobody has
// reviewed stays off the public door after its source moves. Outdated is not a
// review, so the public reader is served the source, never the draft.
func TestAnOutdatedMachineDraftIsNeverServedPublicly(t *testing.T) {
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
		got, err := svc.Translated(ctx, tx, rest.TranslatedQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordIDs: []uuid.UUID{id},
			Sources:  map[uuid.UUID]map[string]string{id: {"title": "About the company."}},
			RichText: map[string]bool{"title": false},
			Public:   true,
		})
		if err != nil {
			t.Fatalf("the public read: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("the public read answered %d records, want 1", len(got))
		}
		if f, ok := got[0].Fields["title"]; ok && f.Value == "Sobre nós." {
			t.Errorf("the public door served an unreviewed machine draft (status %q)", f.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
