package crud_test

// The width of an operand is asked through a range, an equality and a membership list.
// Inequality is the comparison none of them asks, and the one whose wrong answer is not
// an empty page: a count that clamps an operand the column cannot encode, or that drops
// the predicate carrying it, answers every row or none, and the caller reads that as a
// record set. Both halves are asked of one number — 2^31, the first a four-byte count
// column cannot encode — so the refusal belongs to the operand and not to inequality.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestAnInequalityOperandPastTheColumnRefusesTheQuestion counts the rows whose count
// differs from one the column holds, then asks the same question of a number it does not
// hold: the answerable half answers every row but the named one, and the past-the-column
// half is refused with no rows and no total at either door.
func TestAnInequalityOperandPastTheColumnRefusesTheQuestion(t *testing.T) {
	conn := seedProbes(t)
	held := crud.Where{{Field: "count", Op: crud.OpNe, Value: 1}}
	wide := crud.Where{{Field: "count", Op: crud.OpNe, Value: int64(1) << 31}}
	differents := int64(len(probeRows)) - 1
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: held})
		if err != nil || int64(len(rows)) != differents || total != differents {
			t.Fatalf("count <> 1 answered %d of %d (%v), want the %d rows whose count differs",
				len(rows), total, err, differents)
		}
		rows, total, err = crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: wide})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Errorf("count <> %d answered %d of %d (%v), want a refusal with no rows and no total",
				int64(1)<<31, len(rows), total, err)
		}
		if counted, cerr := crud.CountWhere[*Probe](tx, wide); cerr == nil || counted != 0 {
			t.Errorf("the total alone above one operand the column cannot encode answered %d (%v), want a refusal and no answer",
				counted, cerr)
		}
	})
}
