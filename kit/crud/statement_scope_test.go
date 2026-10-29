package crud_test

// A door of this package sends its statement on the handle the transaction gave the
// caller, which is the scope carrying the tenant settings row-level security reads, and
// hands the caller's own context to nothing. A returned row cannot tell the two handles
// apart — both reach the same connection inside the same transaction — so the case reads
// the context the statement itself ran under, where the choice is the only difference.

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// callerMark is the value a caller's context can carry and a transaction's never does,
// so one lookup says whose context a statement ran under.
type callerMark struct{}

// TestGetCtxRunsItsStatementOnTheTransactionContext: GetCtx answers a caller that has
// ended and otherwise sends its SELECT on the transaction's handle whatever the live
// caller carries — a handle rebound to the caller's context would run the read under a
// scope that names no tenant, and the row it answered with would be a bug waiting for a
// policy that reads the context.
func TestGetCtxRunsItsStatementOnTheTransactionContext(t *testing.T) {
	conn := setup(t)
	ids := seed(t, conn, 1)

	var ran []context.Context
	watch := func(d *gorm.DB) { ran = append(ran, d.Statement.Context) }

	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		tx.DB().Callback().Query().Register("pkit.crudtest.context", watch)
		defer tx.DB().Callback().Query().Remove("pkit.crudtest.context")
		marked := context.WithValue(ctx, callerMark{}, "the caller")
		if _, err := crud.GetCtx[*Task](marked, tx, ids[0]); err != nil {
			t.Fatalf("GetCtx: %v", err)
		}
	})
	if len(ran) == 0 {
		t.Fatal("no statement ran under the watch, so the case proved nothing")
	}
	for i, stmt := range ran {
		if _, ok := stmt.Value(callerMark{}).(string); ok {
			t.Errorf("statement %d ran under the caller's context, not the transaction's", i)
		}
	}
}
