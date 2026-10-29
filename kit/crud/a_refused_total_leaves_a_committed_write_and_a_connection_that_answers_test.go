package crud_test

// What the refused transaction still owes is asked, and read, inside itself. Two
// things sit outside that view and a caller meets both: the transaction ends by
// committing, and its connection goes back to the pool for whoever reads next. A
// refusal that left a half-applied write, or a connection that answers the next
// reader with the failed statement rather than its own rows, cannot be seen from
// inside the transaction that was refused.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestARefusedTotalLeavesACommittedWriteAndAConnectionThatAnswersItsOwnQuestion
// counts under an operand past the int4 the count column is, writes in that same
// transaction, and commits. A later transaction over the same pool reads that row
// back by its id and finds the narrowed total holding it: six rows at count 3 or
// above, of which the one written here is the only row the refusal did not seed.
func TestARefusedTotalLeavesACommittedWriteAndAConnectionThatAnswersItsOwnQuestion(t *testing.T) {
	conn := seedProbes(t)
	wide := crud.Where{{Field: "count", Op: crud.OpGte, Value: anOperandPastInt4}}
	narrow := crud.Where{{Field: "count", Op: crud.OpGte, Value: int64(3)}}
	written := &Probe{Label: "owed after the refusal", Body: "written in the refused transaction", Count: 9}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if held, err := crud.CountWhere[*Probe](tx, wide); err == nil || held != 0 {
			t.Fatalf("the total over an operand past the column answered %d (%v), want a refusal and no answer", held, err)
		}
		if err := crud.Create(ctx, tx, written); err != nil {
			t.Fatalf("the write that transaction owed answered %v, want it written", err)
		}
	})
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		got, err := crud.Get[*Probe](tx, written.ID)
		if err != nil {
			t.Fatalf("the row that write owed did not read back after the commit: %v", err)
		}
		if got.Count != 9 {
			t.Errorf("the row that write owed reads count %d, want the 9 it was written with", got.Count)
		}
		if after, aerr := crud.CountWhere[*Probe](tx, narrow); aerr != nil || after != 6 {
			t.Errorf("the same total after that commit answered %d (%v), want the five seeded rows and the one written here", after, aerr)
		}
	})
}
