package translationtest

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

func countMissingTranslations(t *testing.T, f Fixture) {
	translated, _ := f.page("About us", firstParagraph, secondParagraph)
	untouched, _ := f.page("Careers", "We are hiring.", "")
	if err := f.save(translated, translatedBody, "Sobre nós", 0, 0); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rows, counts, total, err := f.Service.Overview(f.Ctx, f.Tx, rest.OverviewQuery{
		Module: TestModule, Entity: TestEntity, Locale: PT, Limit: 10,
	})
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("the overview paged %d of %d records, want both", len(rows), total)
	}
	// The counts are per field, which is what the overview's columns
	// are: two fields × two records, one record complete and one
	// untouched.
	if counts.Missing != 2 || counts.Complete != 2 || counts.Outdated != 0 {
		t.Errorf("the counts are %s, want 2 missing and 2 complete", show(counts))
	}
	for _, row := range rows {
		if row.ID != untouched {
			continue
		}
		if row.Missing != 2 || row.Complete != 0 {
			t.Errorf("the untranslated record reports %d missing and %d complete; a record with no row is missing, and only the entity's own row set can say so",
				row.Missing, row.Complete)
		}
	}
}
