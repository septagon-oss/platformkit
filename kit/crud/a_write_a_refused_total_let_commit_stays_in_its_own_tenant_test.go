package crud_test

// A total this package refuses still owes the transaction it ran in a write, and
// that write reaches the commit. What the refusal narrows is a question and never a
// scope, so the row that commit holds belongs to the tenant that wrote it: the same
// id and the same narrowed total, answered whole for that tenant and answered with
// nothing for another, by the row-level security policy the table carries.

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestAWriteARefusedTotalLetCommitStaysInTheTenantThatWroteIt counts under an
// operand past the int4 the count column is, writes in that same transaction and
// commits, then asks both tenants for that row and for the narrowed total: the
// writer reads the row and six rows in its total, the other tenant is answered
// ErrNotFound and counts none of them.
func TestAWriteARefusedTotalLetCommitStaysInTheTenantThatWroteIt(t *testing.T) {
	conn := seedProbes(t)
	wide := crud.Where{{Field: "count", Op: crud.OpGte, Value: anOperandPastInt4}}
	narrow := crud.Where{{Field: "count", Op: crud.OpGte, Value: int64(3)}}
	written := &Probe{Label: "owed across the refusal", Body: "written where the total was refused", Count: 9}
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
		if err != nil || got.Count != 9 {
			t.Fatalf("the writer reads its own row back as (%+v, %v), want count 9 and no error", got, err)
		}
		if after, aerr := crud.CountWhere[*Probe](tx, narrow); aerr != nil || after != 6 {
			t.Errorf("the writer's narrowed total answered %d (%v), want the five seeded rows and the one written here", after, aerr)
		}
	})
	as(t, conn, globex, func(_ context.Context, tx db.Tx[db.Tenant]) {
		if got, err := crud.Get[*Probe](tx, written.ID); !errors.Is(err, crud.ErrNotFound) || got != nil {
			t.Errorf("another tenant reads that row as (%+v, %v), want ErrNotFound and no row", got, err)
		}
		if other, oerr := crud.CountWhere[*Probe](tx, narrow); oerr != nil || other != 0 {
			t.Errorf("another tenant's narrowed total answered %d (%v), want none of none", other, oerr)
		}
	})
}
