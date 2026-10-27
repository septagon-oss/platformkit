package crud_test

// predicate_narrow_page_test.go asks the list door the questions an equality map
// could never ask: does a page narrowed by a comparison come with the total of that
// same narrowed set, does the id tail still make the order total over it, and do
// row-level security and the lifecycle clause still apply to a read that asked for
// less than everything.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEqualityAndPredicatesNarrowTheSameFieldTogether is the conjunction, read from
// the rows: one equality key and one predicate on the same field hold together, so
// the answer is the intersection and not whichever one was written last. A pair that
// contradicts itself answers no row at all, which is an honest answer to the question
// that was asked and not a refusal.
func TestEqualityAndPredicatesNarrowTheSameFieldTogether(t *testing.T) {
	conn := seedProbes(t)
	cases := []struct {
		name   string
		filter map[string]any
		where  crud.Where
		want   int64
	}{
		{"both narrow the same column", map[string]any{"label": "alpha"},
			crud.Where{{Field: "label", Op: crud.OpIn, Value: []string{"alpha", "beta"}}}, 1},
		{"the same column narrowed to nothing", map[string]any{"count": 3},
			crud.Where{{Field: "count", Op: crud.OpLt, Value: 3}}, 0},
		{"a count and an order on one column", map[string]any{"count": 1},
			crud.Where{{Field: "count", Op: crud.OpLt, Value: 3}}, 1},
		{"two columns, one answer", map[string]any{"flag": true},
			crud.Where{{Field: "count", Op: crud.OpGte, Value: 4}}, 2},
		{"a predicate alone still narrows", nil,
			crud.Where{{Field: "label", Op: crud.OpIn, Value: []string{"alpha", "beta", "Munich"}}}, 3},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, tc := range cases {
			rows, total, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 20, Filter: tc.filter, Where: tc.where})
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
				continue
			}
			if int64(len(rows)) != tc.want || total != tc.want {
				t.Errorf("%s: the page answers %d of %d, want %d", tc.name, len(rows), total, tc.want)
			}
		}
	})
}

// TestANarrowedTotalCountsTheSetTheNarrowedPageCameFrom is the acceptance property,
// asked of every shape of predicate the grammar gained: the number under a narrowed
// page is the count of the set the page was cut from, both by the same builder and
// both against the same rows. A total that drifted from the page would be a screen
// that says there are more rows than it can ever show.
func TestANarrowedTotalCountsTheSetTheNarrowedPageCameFrom(t *testing.T) {
	conn := seedProbes(t)
	mixed := crud.Where{
		{Field: "flag", Op: crud.OpEq, Value: true},
		{Field: "count", Op: crud.OpIn, Value: []int{1, 5, 7}},
	}
	questions := []struct {
		name  string
		where crud.Where
		want  int64
	}{
		{"difference", crud.Where{{Field: "label", Op: crud.OpNe, Value: "alpha"}}, 6},
		{"membership", crud.Where{{Field: "label", Op: crud.OpIn, Value: []string{"beta", "Munich", "münchen"}}}, 3},
		{"absence", crud.Where{{Field: "when", Op: crud.OpIsNull}}, 3},
		{"presence", crud.Where{{Field: "when", Op: crud.OpNotNull}}, 4},
		{"folded case", crud.Where{{Field: "label", Op: crud.OpEqFold, Value: "ALPHA"}}, 2},
		{"an empty list", crud.Where{{Field: "label", Op: crud.OpIn, Value: []string{}}}, 0},
		{"two predicates at once", mixed, 3},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, q := range questions {
			rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: crud.MaxLimit, Where: q.where})
			if err != nil {
				t.Errorf("%s: %v", q.name, err)
				continue
			}
			narrowed, err := crud.CountWhere[*Probe](tx, q.where)
			if err != nil {
				t.Errorf("%s as a total: %v", q.name, err)
				continue
			}
			if pageTotal != q.want || narrowed != q.want || int64(len(rows)) != q.want {
				t.Errorf("%s: the page answers %d rows of %d and the count door %d, want %d from all three",
					q.name, len(rows), pageTotal, narrowed, q.want)
			}
			if err := sameRowsAsThemselves(rows, q.want); err != nil {
				t.Errorf("%s: %v", q.name, err)
			}
		}
	})
}

// sameRowsAsThemselves is the cheap half of "the page is a subset of the counted
// set": the page carries its own rows, so they have to be the number the total gave
// and each one distinct.
func sameRowsAsThemselves(rows []*Probe, want int64) error {
	seen := map[uuid.UUID]bool{}
	for _, r := range rows {
		if seen[r.ID] {
			return errors.New("the page carried one row twice")
		}
		seen[r.ID] = true
	}
	if int64(len(seen)) != want {
		return errors.New("the page's rows are not the number the total gave")
	}
	return nil
}

// TestTheCountAndThePageAreBuiltFromOneWhere reads the two statements themselves: the
// conditions of the total and of the page are the same text with the same number of
// placeholders, and what differs is the page's own ORDER BY, LIMIT and OFFSET. A
// membership list is what makes this the shape that broke: pairing clause i with
// argument i would have bound its first value and dropped the rest into the page only.
func TestTheCountAndThePageAreBuiltFromOneWhere(t *testing.T) {
	conn := seedProbes(t)
	q := crud.Query{
		Limit:  3,
		Offset: 1,
		Sort:   "-count",
		Filter: map[string]any{"flag": true},
		Where: crud.Where{
			{Field: "label", Op: crud.OpIn, Value: []string{"alpha", "beta", "MUNICH", "MÜNCHEN"}},
			{Field: "count", Op: crud.OpGte, Value: 2},
			{Field: "when", Op: crud.OpNotNull},
			{Field: "body", Op: crud.OpEqFold, Value: "fifth"},
		},
	}
	var statements []string
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.oneWhere")
		if _, _, err := crud.ListCtx[*Probe](ctx, tx, q); err != nil {
			t.Fatalf("the narrowed page: %v", err)
		}
		statements = *sent
	})
	if len(statements) != 2 {
		t.Fatalf("one page and its total are %d statements: %v", len(statements), statements)
	}
	total, page := statements[0], statements[1]
	if !strings.Contains(total, "count(") {
		t.Errorf("the first statement is not the total: %q", total)
	}
	if whereOf(total) == "" || whereOf(total) != whereOf(page) {
		t.Errorf("the two statements ask different questions:\n  total %s\n  page  %s", whereOf(total), whereOf(page))
	}
	for _, want := range []string{"ORDER BY", "LIMIT", "OFFSET"} {
		if !strings.Contains(strings.ToUpper(page), want) {
			t.Errorf("the page carries no %s: %q", want, page)
		}
	}
	if !strings.HasSuffix(page, ",id") && !strings.Contains(page, "id") {
		t.Errorf("the page's order does not end in the id: %q", page)
	}
	if strings.Contains(strings.ToUpper(total), "LIMIT") {
		t.Errorf("the total inherited the page's limit: %q", total)
	}
}

// whereOf is the conditions of a statement: everything after WHERE and before the
// page's own clauses. Placeholders are part of the text, so two statements whose
// conditions match ask the same question with the same values in the same order.
func whereOf(sql string) string {
	_, conditions, found := strings.Cut(sql, " WHERE ")
	if !found {
		return ""
	}
	for _, tail := range []string{" ORDER BY ", " LIMIT ", " OFFSET "} {
		if i := strings.Index(conditions, tail); i >= 0 {
			conditions = conditions[:i]
		}
	}
	return conditions
}

// TestAFilteredPageStillEndsItsOrderInTheID pages a narrowed set by a column most of
// the rows agree on. The id tail is what makes that a total order, so the pages
// neither show one row twice nor skip a row, and the narrowed order is the whole
// order with the rows that failed the predicate taken out of it.
func TestAFilteredPageStillEndsItsOrderInTheID(t *testing.T) {
	conn := seedProbes(t)
	flagged := crud.Query{Sort: "flag", Where: crud.Where{{Field: "flag", Op: crud.OpEq, Value: true}}}
	var narrowed, whole []string
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for offset := 0; ; offset += 2 {
			q := flagged
			q.Limit, q.Offset = 2, offset
			rows, total, err := crud.ListCtx[*Probe](ctx, tx, q)
			if err != nil {
				t.Fatalf("page at offset %d: %v", offset, err)
			}
			if total != 4 {
				t.Fatalf("four probes are flagged and the page reports a total of %d", total)
			}
			for _, r := range rows {
				narrowed = append(narrowed, r.Label)
			}
			if len(rows) == 0 {
				break
			}
		}
		rows, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 20, Sort: "flag"})
		if err != nil {
			t.Fatalf("the whole set in the same order: %v", err)
		}
		for _, r := range rows[3:] {
			whole = append(whole, r.Label)
		}
	})
	if strings.Join(narrowed, ",") != strings.Join(whole, ",") {
		t.Errorf("the narrowed pages read %v and the same order without the predicate holds %v", narrowed, whole)
	}
	if len(narrowed) != 4 {
		t.Errorf("the pages returned %d rows, want the four flagged probes and no repeat", len(narrowed))
	}
}

// TestAPredicateSeesNoSoftDeletedRowAndNoOtherTenant narrows a page and its total by
// a membership list and by absence, with a row that is gone and a tenant that is
// elsewhere. The lifecycle clause and the policy are the door's own and a predicate
// cannot widen either: the hidden row is out of the page and out of the number, and
// the same question asked next door answers its own rows.
func TestAPredicateSeesNoSoftDeletedRowAndNoOtherTenant(t *testing.T) {
	conn := seedProbes(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Filter: map[string]any{"label": "beta"}})
		if err != nil || len(rows) != 1 {
			t.Fatalf("the row to hide: %v (%d rows)", err, len(rows))
		}
		if err := crud.SoftDelete[*Probe](ctx, tx, rows[0].ID); err != nil {
			t.Fatalf("soft delete: %v", err)
		}
	})
	// The same three labels next door, where nothing is deleted: globex's own answer
	// to the same question is its own three rows.
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, label := range []string{"alpha", "beta", "Munich"} {
			if err := crud.Create[*Probe](ctx, tx, &Probe{Label: label, Count: 1}); err != nil {
				t.Fatalf("seed globex %s: %v", label, err)
			}
		}
	})
	narrowed := crud.Where{{Field: "label", Op: crud.OpIn, Value: []string{"alpha", "beta", "Munich"}}}
	absent := crud.Where{{Field: "when", Op: crud.OpIsNull}}
	for _, tc := range []struct {
		tenant tenancy.Tenant
		name   string
		want   int64
	}{
		{acme, "acme, whose beta is deleted", 2},
		{globex, "globex, which wrote its own three", 3},
	} {
		as(t, conn, tc.tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) {
			rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 20, Where: narrowed})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			counted, err := crud.CountWhere[*Probe](tx, narrowed)
			if err != nil {
				t.Fatalf("%s narrowed total: %v", tc.name, err)
			}
			if int64(len(rows)) != tc.want || pageTotal != tc.want || counted != tc.want {
				t.Errorf("%s: a membership list answers %d rows of %d and a total of %d, want %d",
					tc.name, len(rows), pageTotal, counted, tc.want)
			}
			for _, r := range rows {
				if r.Label == "beta" && tc.tenant == acme {
					t.Errorf("%s: the soft-deleted row is on the page", tc.name)
				}
			}
		})
	}
	// Absence asked of a moment narrows to the rows that have none, in each tenant
	// and never across them: one of acme's three rows without a moment is the
	// soft-deleted one, so the narrowed total is two and not three.
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Limit: 20, Where: absent})
		if err != nil {
			t.Fatalf("the absent moments: %v", err)
		}
		if int64(len(rows)) != 2 || pageTotal != 2 {
			t.Errorf("absence answers %d rows of a total %d, want the two live probes with no moment", len(rows), pageTotal)
		}
	})
}

// TestARefusedPredicateLeavesTheTransactionUsable is the failure mode a caller feels:
// a question this package can answer from its schema and the query alone is refused
// before anything connects, so the transaction it was asked in is still good, and the
// next question in it is answered rather than refused for a statement nobody sent.
func TestARefusedPredicateLeavesTheTransactionUsable(t *testing.T) {
	conn := seedProbes(t)
	bad := crud.Query{Where: crud.Where{{Field: "due_at", Op: crud.OpEq, Value: "yesterday"}}}
	good := crud.Query{Limit: 20, Where: crud.Where{{Field: "label", Op: crud.OpEqFold, Value: "ALPHA"}}}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.usable")
		_, _, err := crud.ListCtx[*Probe](ctx, tx, bad)
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("the bad question answered %v", err)
		}
		rows, total, err := crud.ListCtx[*Probe](ctx, tx, good)
		if err != nil {
			t.Fatalf("the good question after the refusal: %v", err)
		}
		if len(rows) != 2 || total != 2 {
			t.Errorf("the good question after the refusal answers %d of %d, want the two alpha rows", len(rows), total)
		}
		if len(*sent) != 2 {
			t.Errorf("the pair sent %d statements, want the two of the good question only: %v", len(*sent), *sent)
		}
	})
}
