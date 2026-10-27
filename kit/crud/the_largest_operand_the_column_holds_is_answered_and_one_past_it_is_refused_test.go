package crud_test

// The width of an operand is a boundary, and every case here names one side of it: a
// count past the four-byte column is refused. Nothing asks the other side — that a
// count the column does hold is answered — so a guard that refused too much, the
// mistake a range check most easily makes, would answer every question in this package
// exactly as it always did while refusing the numbers a real record carries. Both sides
// are asked of one number: 2^31-1, the largest a four-byte signed column holds, and one
// past it.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// largestInt4 is the largest count the fixture's int4 count column can hold.
const largestInt4 = int64(1)<<31 - 1

// TestTheLargestOperandTheColumnHoldsIsAnsweredAndOnePastItIsRefused asks the page and
// the total for a count above the largest the column holds — a question with no matching
// rows and a real answer — and then the same question one operand wider, which is
// refused and answers nothing at either door.
func TestTheLargestOperandTheColumnHoldsIsAnsweredAndOnePastItIsRefused(t *testing.T) {
	conn := seedProbes(t)
	inside := crud.Where{{Field: "count", Op: crud.OpGt, Value: largestInt4}}
	past := crud.Where{{Field: "count", Op: crud.OpGt, Value: largestInt4 + 1}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: inside})
		if err != nil || len(rows) != 0 || total != 0 {
			t.Errorf("the page above %d answered %d of %d (%v), want no rows, no total and no error",
				largestInt4, len(rows), total, err)
		}
		if held, herr := crud.CountWhere[*Probe](tx, inside); herr != nil || held != 0 {
			t.Errorf("the total above %d answered %d (%v), want none and no error", largestInt4, held, herr)
		}
		if wider, werr := crud.CountWhere[*Probe](tx, past); werr == nil || wider != 0 {
			t.Errorf("the total above %d answered %d (%v), want a refusal and no answer",
				largestInt4+1, wider, werr)
		}
	})
}
