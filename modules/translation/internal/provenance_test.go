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

// TestTypingOverAMachineDraftKeepsItsProvenance: "a person who types over a
// machine draft keeps origin machine and gains a reviewed_at" (the migration,
// kit/rest's origin constants and the module README all say so).
func TestTypingOverAMachineDraftKeepsItsProvenance(t *testing.T) {
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
		if err := svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Quem somos."}, Expected: map[string]int64{"title": 1},
			Source: src,
		}); err != nil {
			t.Fatalf("typing over the draft: %v", err)
		}
		var row struct {
			Origin   string
			Reviewed bool
		}
		if err := tx.DB().Raw(`SELECT origin, reviewed_at IS NOT NULL AS reviewed FROM translations WHERE record_id = ?`, id).
			Scan(&row).Error; err != nil {
			t.Fatalf("reading the row: %v", err)
		}
		if row.Origin != rest.OriginMachine || !row.Reviewed {
			t.Errorf("after a person typed over the draft: origin %q, reviewed %v; want origin machine, reviewed", row.Origin, row.Reviewed)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
