package crud_test

// predicate_fold_test.go states what eq_fold folds, in the rows rather than in a
// word: the C collation makes it ASCII and nothing else, so it answers the same in
// an installation built against any locale, and a letter outside ASCII keeps its
// own case.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestFoldMatchesAcrossASCIICaseAndRefusesEverythingElse asks the four spellings of
// two Munichs and two MÜNCHENs. Equality still distinguishes case; the fold matches
// the ASCII letters only, so München finds münchen and MÜNCHEN answers only itself —
// the rule, not an accident, and the price of a fold that means the same in every
// installation.
func TestFoldMatchesAcrossASCIICaseAndRefusesEverythingElse(t *testing.T) {
	conn := seedProbes(t)
	cases := []struct {
		op    crud.Op
		ask   string
		want  int64
		label string // the row the answer has to be, when one row answers
	}{
		{crud.OpEq, "Munich", 1, "Munich"},
		{crud.OpEq, "MUNICH", 1, "MUNICH"},
		{crud.OpEqFold, "munich", 2, ""},
		{crud.OpEqFold, "MUNICH", 2, ""},
		{crud.OpEqFold, "München", 1, "münchen"},
		{crud.OpEqFold, "münchen", 1, "münchen"},
		{crud.OpEqFold, "MÜNCHEN", 1, "MÜNCHEN"},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, tc := range cases {
			rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{
				Limit: 10, Where: crud.Where{{Field: "label", Op: tc.op, Value: tc.ask}},
			})
			if err != nil {
				t.Errorf("%s %q: %v", tc.op, tc.ask, err)
				continue
			}
			if int64(len(rows)) != tc.want || total != tc.want {
				t.Errorf("%s %q answers %d rows of a total %d, want %d of seven probes", tc.op, tc.ask, len(rows), total, tc.want)
			}
			if tc.label != "" && (len(rows) != 1 || rows[0].Label != tc.label) {
				t.Errorf("%s %q answers %v, want the one row labelled %q", tc.op, tc.ask, rows, tc.label)
			}
		}
	})
}

// TestFoldOnANonTextFieldIsRefused keeps the fold on the two types that have a case.
// A count, a flag, a moment and a uuid have none, and lower() of a uuid is a cast
// nobody asked for; a refusal is the answer, not an empty page.
func TestFoldOnANonTextFieldIsRefused(t *testing.T) {
	conn := seedProbes(t)
	folded := crud.Where{
		{Field: "count", Op: crud.OpEqFold, Value: "one"},
		{Field: "rate", Op: crud.OpEqFold, Value: "one"},
		{Field: "flag", Op: crud.OpEqFold, Value: "true"},
		{Field: "when", Op: crud.OpEqFold, Value: "yesterday"},
		{Field: "id", Op: crud.OpEqFold, Value: "uuid"},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.fold")
		for _, p := range folded {
			if _, err := crud.CountWhere[*Probe](tx, crud.Where{p}); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("%v answered %v, want a refusal that it names no case", p, err)
			}
		}
		if len(*sent) != 0 {
			t.Errorf("a fold refused by type sent %d statements: %v", len(*sent), *sent)
		}
	})
}
