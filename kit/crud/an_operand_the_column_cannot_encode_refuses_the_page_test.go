package crud_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestAnOperandTheColumnCannotEncodeRefusesThePage asks the page — not the
// total — for a count above the largest the four-byte column holds. An operand the
// column cannot encode is refused before the statement goes out, so neither door answers:
// the page that carries the same predicate owes the same refusal, and a page that
// answered zero rows there would report an empty record set for a question nobody asked.
func TestAnOperandTheColumnCannotEncodeRefusesThePage(t *testing.T) {
	conn := seedProbes(t)
	wide := crud.Where{{Field: "count", Op: crud.OpGt, Value: int64(1) << 31}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: wide})
		if err == nil || len(rows) != 0 || total != 0 {
			t.Errorf("the page above %d answered %d of %d (%v), want a refusal with no rows and no total",
				int64(1)<<31, len(rows), total, err)
		}
	})
}
