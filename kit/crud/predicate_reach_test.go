package crud_test

// predicate_reach_test.go asks what a predicate can reach: another tenant's row
// named by its own id, the operand position of a membership list, and the rows a
// page did not carry. The narrowed total is the number behind the whole narrowed
// set, so a page of it is a page and not the answer.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// TestAMembershipListReachesNoOtherTenantsRow names acme's rows by their primary
// key in a transaction that is not acme's. Row-level security, not a WHERE, is
// what answers, so the same list that is acme's whole table is nobody's table
// next door — at the page and at the total alike.
func TestAMembershipListReachesNoOtherTenantsRow(t *testing.T) {
	conn := seedProbes(t)
	acmeIDs := []uuid.UUID{}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit})
		if err != nil || total != int64(len(probeRows)) || len(rows) != len(probeRows) {
			t.Fatalf("acme reads its own probes: %d of %d (%v)", len(rows), total, err)
		}
		for _, r := range rows {
			acmeIDs = append(acmeIDs, r.ID)
		}
	})
	named := crud.Where{{Field: "id", Op: crud.OpIn, Value: acmeIDs}}
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: named})
		if err != nil {
			t.Fatalf("the membership list named as another tenant: %v", err)
		}
		counted, err := crud.CountWhere[*Probe](tx, named)
		if err != nil {
			t.Fatalf("the same list as a total: %v", err)
		}
		if len(rows) != 0 || pageTotal != 0 || counted != 0 {
			t.Errorf("acme's ids answer %d rows of a total %d and a count of %d, want none of none",
				len(rows), pageTotal, counted)
		}
	})
	// The same list is still acme's seven, so the refusal above is the tenant and
	// not a predicate that stopped matching.
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		counted, err := crud.CountWhere[*Probe](tx, named)
		if err != nil || counted != int64(len(probeRows)) {
			t.Errorf("acme's own ids answer %d (%v), want %d", counted, err, len(probeRows))
		}
	})
}

// TestAMembershipOperandNeverReachesTheStatementAsText puts caller-written text in
// the operand position: a quote, a closing paren, a boolean tautology. The
// statement carries placeholders only, so the tautology answers no row of its own
// and the real label beside it still answers the one row it names.
func TestAMembershipOperandNeverReachesTheStatementAsText(t *testing.T) {
	conn := seedProbes(t)
	escaped := []string{"alpha', 'x') OR 1=1 --", "1) OR (1", "' OR ''"}
	mixed := []string{"alpha", "' OR 1=1 --"}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.operands")
		counted, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "label", Op: crud.OpIn, Value: escaped}})
		if err != nil {
			t.Fatalf("the operands were refused: %v", err)
		}
		if counted != 0 {
			t.Errorf("the escaped-looking list answers %d rows, want none: a tautology is a label nobody wrote", counted)
		}
		rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 10, Where: crud.Where{{Field: "label", Op: crud.OpIn, Value: mixed}}})
		if err != nil {
			t.Fatalf("the same operands beside a real label: %v", err)
		}
		if pageTotal != 1 || len(rows) != 1 || rows[0].Label != "alpha" {
			t.Errorf("the list with one real label answers %d rows of a total %d, want alpha alone",
				len(rows), pageTotal)
		}
		if len(*sent) != 3 {
			t.Fatalf("the pair sent %d statements, want the total and the page's two", len(*sent))
		}
		for _, sql := range *sent {
			if strings.Contains(strings.ToLower(sql), " or ") {
				t.Errorf("an operand reached the statement text: %q", sql)
			}
			if !strings.Contains(sql, "IN ($1") {
				t.Errorf("the membership list is not spelled with placeholders: %q", sql)
			}
			if n := strings.Count(sql, "$"); n < 2 || n > 4 {
				t.Errorf("%q carries %d placeholders, want one per listed value plus the page's own", sql, n)
			}
			for _, w := range append(escaped, mixed[1]) {
				if strings.Contains(sql, w) {
					t.Errorf("the operand %q is in the statement text %q", w, sql)
				}
			}
		}
	})
}

// TestANarrowedTotalOutlivesItsPage asks the narrowed read at a page smaller than
// the set: the total is the whole narrowed set and never the page's length, and the
// pages between them neither repeat a row nor lose one. Where the whole question is
// one builder, the number and the rows have to agree at every page size.
func TestANarrowedTotalOutlivesItsPage(t *testing.T) {
	conn := seedProbes(t)
	// Four live probes carry a moment; three carry none, one of them by no accident.
	where := crud.Where{{Field: "when", Op: crud.OpNotNull}}
	const want = 4
	const pageSize = 2
	seen := map[uuid.UUID]bool{}
	for page := 0; page < 3; page++ {
		as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
			rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{
				Limit: pageSize, Offset: page * pageSize, Sort: "count", Where: where,
			})
			if err != nil {
				t.Fatalf("page %d: %v", page+1, err)
			}
			if total != want {
				t.Errorf("page %d reports a total of %d, want the whole narrowed set of %d",
					page+1, total, want)
			}
			wantRows := min(pageSize, want-page*pageSize)
			if len(rows) != wantRows {
				t.Errorf("page %d carries %d rows, want %d", page+1, len(rows), wantRows)
			}
			for _, r := range rows {
				if seen[r.ID] {
					t.Errorf("page %d carries row %s a second time", page+1, r.ID)
				}
				seen[r.ID] = true
				if r.When == nil {
					t.Errorf("page %d carries %s, which has no moment", page+1, r.Label)
				}
			}
		})
	}
	if len(seen) != want {
		t.Errorf("the pages between them carry %d distinct rows, want %d", len(seen), want)
	}
}

// TestAQueryThatNarrowsTwiceOnOneColumnRefusesNeitherAnswer answers the pair a
// caller can build by accident — equality and a comparison on one column that
// cannot both hold. The conjunction is what was written, so the answer is no rows,
// and the door that counts them says so rather than dropping one condition.
func TestAQueryThatNarrowsTwiceOnOneColumnRefusesNeitherAnswer(t *testing.T) {
	conn := seedProbes(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.twice")
		_, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{
			Filter: map[string]any{"count": 5},
			Where:  crud.Where{{Field: "count", Op: crud.OpGt, Value: 5}},
		})
		if err != nil {
			t.Fatalf("the contradicting pair was refused: %v", err)
		}
		if total != 0 {
			t.Errorf("the contradicting pair answers %d rows, want none", total)
		}
		counted, err := crud.CountWhere[*Probe](tx, crud.Where{
			{Field: "count", Op: crud.OpEq, Value: 5},
			{Field: "count", Op: crud.OpGt, Value: 5},
		})
		if err != nil {
			t.Fatalf("the pair as a total: %v", err)
		}
		if counted != 0 {
			t.Errorf("the pair as a total answers %d, want none", counted)
		}
		// And the pair that does hold, held together: the equality narrows to the
		// one row and the comparison leaves it in.
		_, both, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{
			Filter: map[string]any{"count": 5},
			Where:  crud.Where{{Field: "count", Op: crud.OpGte, Value: 5}},
		})
		if err != nil || both != 1 {
			t.Errorf("the holding pair answers %d (%v), want the one row", both, err)
		}
		if len(*sent) != 5 {
			t.Errorf("the three questions sent %d statements, want five", len(*sent))
		}
	})
}

// TestAnInOperandOfTheWrongShapeIsRefusedForEveryElement: a list whose first value
// fits and second does not must not narrow to the half it could bind.
func TestAnInOperandOfTheWrongShapeIsRefusedForEveryElement(t *testing.T) {
	conn := seedProbes(t)
	bad := []crud.Where{
		{{Field: "count", Op: crud.OpIn, Value: []any{1, "two"}}},
		{{Field: "count", Op: crud.OpIn, Value: []any{1, nil}}},
		{{Field: "id", Op: crud.OpIn, Value: []any{uuid.New(), "not-a-uuid"}}},
		{{Field: "when", Op: crud.OpIn, Value: []any{probeDay, "yesterday"}}},
	}
	as(t, conn, acme, func(_ context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.badlist")
		for _, w := range bad {
			n, err := crud.CountWhere[*Probe](tx, w)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("%v answered %d rows and %v, want a refusal", w, n, err)
			}
		}
		if len(*sent) != 0 {
			t.Errorf("a list with one bad element sent %d statements: %v", len(*sent), *sent)
		}
	})
}

// word is a label written as its own type, the shape a module's own named string
// arrives in.
type word string

// TestAnEqualityOperandOfTheShapeTheColumnCannotHoldIsRefused pins the tightening
// the equality map came home with: an operand is now read against the column, so a
// moment spelled as text, a flag spelled as text and a named string that is no
// string to the column all refuse with ErrInvalid instead of reaching a statement.
// An integer column takes any Go integer kind by contrast, so the rule is by kind
// for a number and by exact type for a word, a flag, a moment and an id.
func TestAnEqualityOperandOfTheShapeTheColumnCannotHoldIsRefused(t *testing.T) {
	conn := seedProbes(t)
	type asked struct {
		field string
		value any
	}
	bad := []asked{
		{"when", "2026-01-01"},
		{"count", "3"},
		{"flag", "true"},
		{"id", "00000000-0000-0000-0000-000000000000"},
		{"label", word("alpha")},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.equalityShape")
		for _, a := range bad {
			p := crud.Predicate{Field: a.field, Op: crud.OpEq, Value: a.value}
			if _, err := crud.CountWhere[*Probe](tx, crud.Where{p}); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("%s = %#v answered %v, want a refusal that the column cannot hold it", a.field, a.value, err)
			}
			if _, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Filter: map[string]any{a.field: a.value}}); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("filter %s=%#v answered %v, want a refusal", a.field, a.value, err)
			}
		}
		if len(*sent) != 0 {
			t.Errorf("an operand the column cannot hold sent %d statements: %v", len(*sent), *sent)
		}
		// The same columns asked with the shape each holds answer, so the refusals
		// above are the operand and not the door.
		n, err := crud.CountWhere[*Probe](tx, crud.Where{
			{Field: "label", Op: crud.OpEq, Value: "alpha"},
			{Field: "count", Op: crud.OpEq, Value: int64(1)},
			{Field: "flag", Op: crud.OpEq, Value: true},
		})
		if err != nil || n != 1 {
			t.Errorf("the exact-typed question answers %d (%v), want alpha", n, err)
		}
	})
}
