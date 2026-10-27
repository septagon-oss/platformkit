package crud_test

// A number inside the admitted Go kind and past the column's own width is refused
// below this package, so what matters here is that no door answers a partial
// question: the two spellings of equality have to fail alike, and a membership list
// carrying one such value has to refuse the whole question rather than answer the
// values it could encode.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// pastInt4 is past a four-byte count column and inside a Go int64, which is the
// shape entity.Coerce hands a filter read off a request.
const pastInt4 = int64(1) << 40

// TestATooWideOperandRefusesTheWholeQuestionWhicheverSpellingAskedIt narrows by
// pastInt4 through the filter map, through an OpEq predicate and through an In list
// that holds one answerable value beside it.
func TestATooWideOperandRefusesTheWholeQuestionWhicheverSpellingAskedIt(t *testing.T) {
	conn := seedProbes(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		mapRows, mapTotal, mapErr := crud.ListCtx[*Probe](ctx, tx,
			crud.Query{Limit: crud.MaxLimit, Filter: map[string]any{"count": pastInt4}})
		eqRows, eqTotal, eqErr := crud.ListCtx[*Probe](ctx, tx,
			crud.Query{Limit: crud.MaxLimit, Where: crud.Where{{Field: "count", Op: crud.OpEq, Value: pastInt4}}})
		if mapErr == nil || eqErr == nil {
			t.Errorf("a count no column can hold: map %v, predicate %v", mapErr, eqErr)
		}
		if len(mapRows) != len(eqRows) || mapTotal != eqTotal {
			t.Errorf("the two spellings of one equality answered differently: map %d of %d, predicate %d of %d",
				len(mapRows), mapTotal, len(eqRows), eqTotal)
		}
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit,
			Where: crud.Where{{Field: "count", Op: crud.OpIn, Value: []int64{1, pastInt4}}}})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Errorf("a membership list narrowed to the value it could encode: %d of %d (%v)", len(rows), total, err)
		}
		held, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "count", Op: crud.OpIn, Value: []int64{1, 2}}})
		if err != nil || held != 2 {
			t.Errorf("the same list without that value: %d (%v)", held, err)
		}
	})
}
