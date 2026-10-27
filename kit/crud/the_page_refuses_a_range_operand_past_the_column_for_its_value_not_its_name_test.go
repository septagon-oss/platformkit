package crud_test

// A page that refuses an operand its count column cannot encode is evidence only while
// the same page answers the same question with an operand it can: an unknown field is
// refused too, so a case that asks only the number past the column stays green when the
// field is misspelled and the refusal stops meaning anything. Both numbers are asked
// here of one field and one operator at the page door, and the answerable half answers a
// non-empty set, so a dropped predicate, a clamped operand or a lost field all fail it.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// rangeOperandPastInt4 is the first count the fixture's four-byte count column cannot
// hold; the held operand below it is a count the column holds and two rows sit above it.
const rangeOperandPastInt4 = int64(1) << 31

// TestThePageRefusesARangeOperandPastTheColumnForItsValueNotItsName asks the page for the
// counts above a number the column holds and then for the counts above one it does not:
// the first answers the two rows it names, the second is refused with no rows and no
// total, and a field the entity does not carry is refused the same way, so the refusal
// follows from the operand's width rather than from how the question is spelled.
func TestThePageRefusesARangeOperandPastTheColumnForItsValueNotItsName(t *testing.T) {
	conn := seedProbes(t)
	held := crud.Where{{Field: "count", Op: crud.OpGt, Value: 5}}
	past := crud.Where{{Field: "count", Op: crud.OpGt, Value: rangeOperandPastInt4}}
	unnamed := crud.Where{{Field: "coust", Op: crud.OpGt, Value: 5}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: held})
		if err != nil || len(rows) != 2 || total != 2 {
			t.Fatalf("the page above 5 answered %d of %d (%v), want the 2 rows whose count is above it",
				len(rows), total, err)
		}
		wideRows, wideTotal, wideErr := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: past})
		if wideErr == nil || len(wideRows) != 0 || wideTotal != 0 {
			t.Errorf("the page above %d answered %d of %d (%v), want a refusal with no rows and no total",
				rangeOperandPastInt4, len(wideRows), wideTotal, wideErr)
		}
		badRows, badTotal, badErr := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: unnamed})
		if badErr == nil || len(badRows) != 0 || badTotal != 0 {
			t.Errorf("the page on a field the entity does not carry answered %d of %d (%v), want a refusal with no rows and no total",
				len(badRows), badTotal, badErr)
		}
	})
}
