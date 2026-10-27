package crud_test

// operand_width_test.go asks the one thing the operand rule leaves below this
// package: the operand's kind is refused here and its range is not. A column
// narrower than the Go number that reads it — what any hand-written migration
// narrower than its entity is — holds no answer to a number inside the Go kind and
// past the column, so the read answers an error and no rows at either door, and the
// transaction is still usable afterwards for the question the column can answer.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// pastColumn is 2^40: inside a Go int, which is what Probe.Count is, and outside the
// four-byte count column the fixture's own DDL creates.
const pastColumn = uint64(1) << 40

// TestAnOperandPastTheWidthOfTheColumnAnswersAnErrorAndNoRows narrows by a number no
// column of that type can hold. Nothing about it answers a page: the total, the page
// and the count alone each fail, none of them answers zero rows as though the question
// had been asked and nothing matched, and the transaction goes on serving.
func TestAnOperandPastTheWidthOfTheColumnAnswersAnErrorAndNoRows(t *testing.T) {
	conn := seedProbes(t)
	tooWide := crud.Where{{Field: "count", Op: crud.OpLt, Value: pastColumn}}
	live := crud.Where{{Field: "count", Op: crud.OpGte, Value: 1}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: tooWide})
		if err == nil || pageTotal != 0 || len(rows) != 0 {
			t.Errorf("the page asked below %d: %d of %d (%v)", pastColumn, len(rows), pageTotal, err)
		}
		counted, err := crud.CountWhere[*Probe](tx, tooWide)
		if err == nil || counted != 0 {
			t.Errorf("the total alone asked below %d: %d (%v)", pastColumn, counted, err)
		}
		counted, err = crud.CountWhere[*Probe](tx, live)
		if err != nil || counted != int64(len(probeRows)) {
			t.Errorf("the question this column holds, asked after the one it does not: %d (%v)", counted, err)
		}
	})
}
