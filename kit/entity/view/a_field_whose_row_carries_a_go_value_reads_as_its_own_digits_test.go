package view_test

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFieldWhoseRowCarriesAGoValueReadsAsItsOwnDigits reads `text`, the member a screen
// prints, for a row that carries the Go value a handler holds rather than the decoded JSON
// the wire gives: an int, an int64 past what a double holds, and a time.Time. The value
// member already spells these whole, so a reading that fell back to the plain text of a
// carrier it does not know would print an empty cell beside a record whose own JSON names
// the figure — and the digits a shell must not round are the ones this domain promises.
// Both members are read as the Page writes them, so what is checked reaches a shell
// rather than living in one Go field.
func TestAFieldWhoseRowCarriesAGoValueReadsAsItsOwnDigits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		field    string
		stored   any
		kind     view.ValueKind
		text     string
		spelling string
	}{
		{"count", int64(42), view.ValueInteger, "42", `42`},
		{"count", int(-7), view.ValueInteger, "-7", `-7`},
		{"count", int64(9007199254740992), view.ValueBigInteger, "9007199254740992", `"9007199254740992"`},
		{"dueAt", time.Date(2026, 7, 1, 14, 12, 0, 0, time.UTC), view.ValueInstant,
			"2026-07-01T14:12:00.000000Z", `"2026-07-01T14:12:00.000000Z"`},
	} {
		fields := recordCells(t, domainInput(map[string]any{
			"id": "t1", "title": "n", tc.field: tc.stored,
		}, time.UTC))
		got, ok := fields[tc.field]
		if !ok {
			t.Fatalf("%s is not on the page, so this case would inspect nothing", tc.field)
		}
		if got.ValueKind != tc.kind {
			t.Errorf("%s of %T is %s, want %s", tc.field, tc.stored, got.ValueKind, tc.kind)
		}
		if got.Text != tc.text {
			t.Errorf("%s of %T reads %q, want the digits a screen prints: %q",
				tc.field, tc.stored, got.Text, tc.text)
		}
		if spelling := string(bytes(t, got.Value)); spelling != tc.spelling {
			t.Errorf("%s of %T spells its value as %s, want %s", tc.field, tc.stored, spelling, tc.spelling)
		}
	}
}
