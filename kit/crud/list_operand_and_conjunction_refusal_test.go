package crud_test

// Two corners of the operand rule answer no door on their own. An equality key that
// carries a list is the map spelling of a question nobody asked: read as equality it
// compares nothing, read as membership it answers rows whose caller never chose an
// operator, so the map stays equality and refuses. And one valid condition beside an
// operand the column cannot encode is still one question — the conjunction is what the
// caller wrote, so answering the half that encodes would answer a wider set than was
// asked for. Neither refusal reaches Postgres, so the transaction committing at the
// end of each case is the proof the refusal happened on the way to the wire.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestAnEqualityKeyCarryingAListOfValuesIsRefused asks the filter map with one key
// whose value is a list — numbers for a count column, words for a text column. The map
// is equality and does not become a membership test, at the page or the total it would
// have carried. The same values as a membership predicate answer, so the refusals are
// the map's equality and not the values in it.
func TestAnEqualityKeyCarryingAListOfValuesIsRefused(t *testing.T) {
	conn := seedProbes(t)
	lists := []map[string]any{
		{"count": []int64{1, 2}},
		{"label": []string{"alpha", "beta"}},
	}
	listed := int64(0)
	for _, r := range probeRows {
		if r.count == 1 || r.count == 2 {
			listed++
		}
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, filter := range lists {
			rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Filter: filter})
			if !errors.Is(err, crud.ErrInvalid) || len(rows) != 0 || total != 0 {
				t.Errorf("the equality map carrying %#v answered %d of %d (%v), want a refusal and no answer",
					filter, len(rows), total, err)
			}
		}
		held, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "count", Op: crud.OpIn, Value: []int64{1, 2}}})
		if err != nil || held != listed {
			t.Errorf("the same values as a membership predicate answered %d (%v), want the %d rows with those counts",
				held, err, listed)
		}
	})
}

// TestAValidConditionBesideAnOperandTheColumnCannotHoldRefusesTheWholeQuestion asks one
// question whose equality names a row and whose membership list carries one value past
// the count column's width. The page, the total beside it and the count alone all
// refuse and answer nothing; the same question with that value removed answers the one
// row the label names — one, because the table cannot hold two rows of one label for
// one tenant. So the refusals are that one value and not the conjunction failing.
func TestAValidConditionBesideAnOperandTheColumnCannotHoldRefusesTheWholeQuestion(t *testing.T) {
	conn := seedProbes(t)
	wide := []int64{1, pastInt4}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{
			Limit: crud.MaxLimit, Filter: map[string]any{"label": "alpha"},
			Where: crud.Where{{Field: "count", Op: crud.OpIn, Value: wide}},
		})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Errorf("an equality beside a list the column cannot encode answered %d of %d (%v), want no answer at either door",
				len(rows), total, err)
		}
		held, err := crud.CountWhere[*Probe](tx, crud.Where{
			{Field: "label", Op: crud.OpEq, Value: "alpha"},
			{Field: "count", Op: crud.OpIn, Value: wide},
		})
		if err == nil || held != 0 {
			t.Errorf("the same question as a total alone answered %d (%v), want no answer", held, err)
		}
		kept, err := crud.CountWhere[*Probe](tx, crud.Where{
			{Field: "label", Op: crud.OpEq, Value: "alpha"},
			{Field: "count", Op: crud.OpIn, Value: []int64{1}},
		})
		if err != nil || kept != 1 {
			t.Errorf("the same question with that value removed answered %d (%v), want the one row alpha names", kept, err)
		}
	})
}
