package crud_test

// A total refused for an operand the column cannot hold is refused before the
// statement leaves the process, so the transaction that asked it is open and owes
// the rest of the module's work. A page is not the only thing it owes: the caller
// that counts before it writes expects that write, and expects the same question
// back with an operand the column holds — the first shape a retry takes. If the
// refusal ever came from Postgres instead of the encoder, the abort answers the
// narrowed total; if the failed statement stayed poisoned in the connection, that
// total answers the poison rather than its own rows.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestARefusedTotalStillOwesThatTransactionAWriteAndItsOwnNarrowerTotal asks one
// total over an operand past the int4 the count column is, then the same total over
// 3, then a write, then that total again. The refusal answers nothing, the narrowed
// total answers the five seeded rows whose count reaches 3, and the write lands in
// the same transaction, making that total six.
func TestARefusedTotalStillOwesThatTransactionAWriteAndItsOwnNarrowerTotal(t *testing.T) {
	conn := seedProbes(t)
	wide := crud.Where{{Field: "count", Op: crud.OpGte, Value: anOperandPastInt4}}
	narrow := crud.Where{{Field: "count", Op: crud.OpGte, Value: int64(3)}}
	written := &Probe{Label: "after the refusal", Body: "the transaction was still open", Count: 9}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if held, err := crud.CountWhere[*Probe](tx, wide); err == nil || held != 0 {
			t.Fatalf("the total over an operand past the column answered %d (%v), want a refusal and no answer", held, err)
		}
		held, err := crud.CountWhere[*Probe](tx, narrow)
		if err != nil || held != 5 {
			t.Fatalf("that total with an operand the column holds answered %d (%v), want the five seeded rows at count 3 or above", held, err)
		}
		if cerr := crud.Create(ctx, tx, written); cerr != nil {
			t.Fatalf("the write that transaction still owed answered %v, want it written", cerr)
		}
		if after, aerr := crud.CountWhere[*Probe](tx, narrow); aerr != nil || after != 6 {
			t.Errorf("the total after that write answered %d (%v), want the five seeded rows and the one written here", after, aerr)
		}
	})
}
