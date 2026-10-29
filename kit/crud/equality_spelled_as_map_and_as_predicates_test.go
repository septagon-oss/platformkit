package crud_test

// One question written two ways has to be one question. Query.Filter's keys and the
// OpEq predicates CountWhere takes are read by the same walk, so the statement the
// page's total is cut with and the statement the total alone is cut with are the
// same text, and the equality keys are cut into it before the predicates and in
// sorted field order whatever order a map happened to be built in.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

func TestEqualitySpelledAsAMapAndAsPredicatesBuildsOneStatement(t *testing.T) {
	conn := seedProbes(t)
	// Three equality keys out of map order, plus the comparison a map cannot say.
	filter := map[string]any{"label": "alpha", "count": 1, "flag": true}
	where := crud.Where{{Field: "when", Op: crud.OpNotNull}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.twoForms")
		rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 2, Filter: filter, Where: where})
		if err != nil || pageTotal != 1 || len(rows) != 1 {
			t.Fatalf("the map and the predicate together: %d of %d (%v)", len(rows), pageTotal, err)
		}
		counted, err := crud.CountWhere[*Probe](tx, crud.Where{
			{Field: "count", Op: crud.OpEq, Value: 1},
			{Field: "flag", Op: crud.OpEq, Value: true},
			{Field: "label", Op: crud.OpEq, Value: "alpha"},
			{Field: "when", Op: crud.OpNotNull},
		})
		if err != nil || counted != pageTotal {
			t.Fatalf("the same question as predicates alone: %d of %d (%v)", counted, pageTotal, err)
		}
		if len(*sent) != 3 {
			t.Fatalf("the pair asked %d statements, want the page's total and page and the count alone", len(*sent))
		}
		if (*sent)[0] != (*sent)[2] {
			t.Errorf("the page's total and the total alone are two statements:\n  %s\n  %s", (*sent)[0], (*sent)[2])
		}
		page := (*sent)[1]
		at := []int{
			strings.Index(page, "count = $1"),
			strings.Index(page, "flag = $2"),
			strings.Index(page, "label = $3"),
			strings.Index(page, "when_at IS NOT NULL"),
		}
		for i, n := range at {
			if n < 0 {
				t.Fatalf("the page's clause %d of four is missing from %q", i+1, page)
			}
			if i > 0 && at[i] < at[i-1] {
				t.Errorf("the equality key %d is cut after the one before it in %q", i+1, page)
			}
		}
	})
}
