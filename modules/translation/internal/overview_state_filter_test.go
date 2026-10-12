package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// TestTheOverviewKeepsOnlyRecordsInTheStateAskedFor: OverviewQuery.State keeps
// the records with at least one field in that state, and the total is the total
// after the filter — the overview's "Missing" filter of journey 4.
func TestTheOverviewKeepsOnlyRecordsInTheStateAskedFor(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	source := translationtest.NewStubSource()
	source.FieldList = []string{"title"}
	source.Rich = map[string]bool{"title": false}
	svc := internal.NewService([]rest.TranslationSource{source}, nil)
	done, todo := uuid.New(), uuid.New()
	source.Put(done, time.Now().UTC(), map[string]string{"title": "About us."})
	source.Put(todo, time.Now().UTC(), map[string]string{"title": "Contact."})

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: done,
			Values: map[string]string{"title": "Sobre nós."}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."}, RichText: map[string]bool{"title": false},
		}); err != nil {
			t.Fatalf("translating one record: %v", err)
		}
		rows, _, total, err := svc.Overview(ctx, tx, rest.OverviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", State: rest.StateMissing,
			Languages: []string{"en", "pt-PT"},
		})
		if err != nil {
			t.Fatalf("the overview: %v", err)
		}
		if len(rows) != 1 || rows[0].ID != todo || total != 1 {
			ids := make([]uuid.UUID, 0, len(rows))
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			t.Errorf("filtered by missing: rows %v, total %d; want only %v, total 1", ids, total, todo)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
