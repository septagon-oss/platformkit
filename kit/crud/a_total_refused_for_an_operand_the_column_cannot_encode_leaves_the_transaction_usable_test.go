package crud_test

// The total alone is the door a module reaches first when it counts before it
// pages, and it is the door no page case opens: an operand the column cannot hold
// has to be refused there without reaching the server, because a statement Postgres
// refuses aborts the transaction it ran in and every later command in that
// transaction then fails for a question nobody asked. This asks the total first,
// under an order and then under an equality, and answers a second question after
// both, so a refusal that ever comes back from the server rather than from the
// encoder shows up as the abort and not as a passing count.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestATotalRefusedForAnOperandTheColumnCannotEncodeLeavesTheTransactionUsable counts
// before it pages: a membership-free question over an ordered column and then one
// over an equal column, each carrying one operand past the column's width, then the
// question the transaction still owes. Both totals refuse and answer nothing, and the
// question after them is answered rather than aborted.
func TestATotalRefusedForAnOperandTheColumnCannotEncodeLeavesTheTransactionUsable(t *testing.T) {
	conn := seedProbes(t)
	following := crud.Query{Limit: crud.MaxLimit, Where: crud.Where{{Field: "label", Op: crud.OpEqFold, Value: "MUNICH"}}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, wide := range []crud.Where{
			{{Field: "count", Op: crud.OpGt, Value: anOperandPastInt4}},
			{{Field: "count", Op: crud.OpEq, Value: anOperandPastInt4}, {Field: "flag", Op: crud.OpEq, Value: true}},
		} {
			held, err := crud.CountWhere[*Probe](tx, wide)
			if err == nil || held != 0 {
				t.Fatalf("the total alone for %s %s answered %d (%v), want a refusal and no answer",
					wide[0].Field, wide[0].Op, held, err)
			}
		}
		live, liveTotal, ferr := crud.ListCtx[*Probe](ctx, tx, following)
		if ferr != nil {
			t.Fatalf("the next question in that transaction answered %v, want the rows MUNICH folds onto", ferr)
		}
		if len(live) != 2 || liveTotal != 2 {
			t.Errorf("the next question in that transaction answers %d rows of a total %d, want the two ASCII rows MUNICH folds onto",
				len(live), liveTotal)
		}
	})
}
