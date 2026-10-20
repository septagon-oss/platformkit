package contracts_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestDistinctRecordsCountsRecordsAndNotRows asks the count Delete names when it
// refuses to be the number of rows in the ledger. One page showing one picture in
// two fields in two locales is one page to go and edit, and a refusal that answers
// four about it is a refusal nobody can act on.
func TestDistinctRecordsCountsRecordsAndNotRows(t *testing.T) {
	page, other, third := uuid.New(), uuid.New(), uuid.New()
	row := func(module, entity, field, locale string, record uuid.UUID) contracts.UseRow {
		return contracts.UseRow{Use: contracts.Use{
			Module: module, Entity: entity, Record: record, Field: field, Locale: locale,
		}}
	}
	cases := []struct {
		name string
		rows []contracts.UseRow
		want int
	}{
		{"nothing", nil, 0},
		{"one page, two fields, two locales", []contracts.UseRow{
			row("content", "page", "body", "en", page),
			row("content", "page", "summary", "en", page),
			row("content", "page", "body", "fr", page),
		}, 1},
		{"three pages over two modules", []contracts.UseRow{
			row("content", "page", "body", "en", page),
			row("content", "post", "body", "en", other),
			row("site", "logo", "body", "en", third),
		}, 3},
		{"the same use twice is one use", []contracts.UseRow{
			row("content", "page", "body", "en", page),
			row("content", "page", "body", "en", page),
		}, 1},
	}
	for _, one := range cases {
		if got := contracts.DistinctRecords(one.rows); got != one.want {
			t.Errorf("%s: DistinctRecords = %d, want %d", one.name, got, one.want)
		}
	}
}
