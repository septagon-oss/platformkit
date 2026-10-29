package crud_test

// Where the operand's Go shape is wrong, this package refuses from its schema
// before anything connects. Where the shape is right and the number is wider than
// the column, the encoder on the wire refuses it, and the two refusals feel the same
// to a caller only while nothing was actually sent: a statement Postgres refuses
// aborts the transaction it ran in, and every later command in that transaction then
// fails for a question nobody asked. So "refused" is checked here as "the rest of the
// work still answers", on the door this package does not own.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// anOperandPastInt4 is a count the count column cannot hold: the column is int4 and
// the operand is a valid int64, so the refusal belongs to the encoder and not to the
// type rule this package reads off its schema.
const anOperandPastInt4 = int64(1) << 40

// TestAQuestionRefusedForAnOperandTheColumnCannotEncodeLeavesTheTransactionUsable asks
// one question whose equality names a row and whose membership list carries one value
// past the column's width, at the page and at the total alone, and answers a second
// question in the same transaction. Both doors refuse and answer nothing, and the
// second question is answered: had either refusal come back from Postgres instead of
// from the encoder, the transaction would be aborted and the second question would
// answer the abort rather than its own rows.
func TestAQuestionRefusedForAnOperandTheColumnCannotEncodeLeavesTheTransactionUsable(t *testing.T) {
	conn := seedProbes(t)
	wide := crud.Where{
		{Field: "label", Op: crud.OpEq, Value: "alpha"},
		{Field: "count", Op: crud.OpIn, Value: []int64{1, anOperandPastInt4}},
	}
	following := crud.Query{Limit: crud.MaxLimit, Where: crud.Where{{Field: "label", Op: crud.OpEqFold, Value: "ALPHA"}}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: wide})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Fatalf("the page answered %d of %d (%v), want a refusal and no answer", len(rows), total, err)
		}
		if held, cerr := crud.CountWhere[*Probe](tx, wide); cerr == nil || held != 0 {
			t.Fatalf("the total alone answered %d (%v), want a refusal and no answer", held, cerr)
		}
		live, liveTotal, ferr := crud.ListCtx[*Probe](ctx, tx, following)
		if ferr != nil {
			t.Fatalf("the next question in that transaction answered %v, want the two rows ALPHA folds onto", ferr)
		}
		if len(live) != 2 || liveTotal != 2 {
			t.Errorf("the next question in that transaction answers %d rows of a total %d, want the two rows ALPHA folds onto",
				len(live), liveTotal)
		}
	})
}
