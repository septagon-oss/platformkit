package crud_test

// predicate_operators_test.go asks the operator table from both sides: what it
// admits answers with the rows the data holds, and what it refuses answers with a
// sentence and no statement at all. The refusals are the point of the exercise — a
// comparison a column cannot answer has to be the caller's error rather than a
// query that quietly matches nothing, or worse, everything.

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// Probe is the fixture the operator table is read against: one column of every type
// a schema can hold a question about, plus a list to be refused for all ten.
type Probe struct {
	crud.Base
	Label string     `json:"label"`
	Body  string     `json:"body" gorm:"type:text"`
	Count int        `json:"count,omitempty"`
	Rate  float64    `json:"rate,omitempty"`
	Flag  bool       `json:"flag,omitempty"`
	When  *time.Time `json:"when,omitempty" gorm:"column:when_at"`
	Tags  []string   `json:"tags,omitempty" gorm:"-"`
}

func (Probe) TableName() string { return "crud_probes" }

// probeDDL carries the same row-level security the tasks table does: the narrowing
// is a predicate and never a tenant, and the policy is what keeps that true.
const probeDDL = `
CREATE TABLE crud_probes (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	label text NOT NULL,
	body text NOT NULL DEFAULT '',
	count int NOT NULL DEFAULT 0,
	rate double precision NOT NULL DEFAULT 0,
	flag boolean NOT NULL DEFAULT false,
	when_at timestamptz,
	UNIQUE (tenant_id, label)
);
ALTER TABLE crud_probes ENABLE ROW LEVEL SECURITY;
ALTER TABLE crud_probes FORCE ROW LEVEL SECURITY;
CREATE POLICY crud_probes_tenant ON crud_probes
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// probeDay is the UTC day the probe moments are measured from.
var probeDay = crud.DayStart(time.Now().UTC())

// probeRows is the data the table is read against: seven live rows whose label,
// count, rate and flag are all different, three of which carry no moment at all.
var probeRows = []struct {
	label, body string
	count       int
	rate        float64
	flag        bool
	hour        int // hours past probeDay; a negative hour is a row with no moment
}{
	{"alpha", "first", 1, 1.5, true, 9},
	{"Alpha", "second", 2, 2.5, false, 26},
	{"beta", "third", 3, 3.5, true, -1},
	{"Munich", "fourth", 4, 4.5, false, -1},
	{"MUNICH", "fifth", 5, 5.5, true, 50},
	{"münchen", "sixth", 6, 6.5, false, 74},
	{"MÜNCHEN", "seventh", 7, 7.5, true, -1},
}

// seedProbes writes probeRows as acme and hands back the application connection, so
// a case reads the table through row-level security the way a module does.
func seedProbes(t *testing.T) *db.Conn {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), probeDDL); err != nil {
		t.Fatalf("create probes: %v", err)
	}
	as(t, app, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, r := range probeRows {
			e := &Probe{Label: r.label, Body: r.body, Count: r.count, Rate: r.rate, Flag: r.flag}
			if r.hour >= 0 {
				at := probeDay.Add(time.Duration(r.hour) * time.Hour)
				e.When = &at
			}
			if err := crud.Create[*Probe](ctx, tx, e); err != nil {
				t.Fatalf("seed %s: %v", r.label, err)
			}
		}
	})
	return app
}

// watchStatements keeps the SQL text of this transaction's SELECTs. The clause text
// is where two statements are compared, and it carries placeholders and never a
// value, so a case can read it without printing a record's own data.
func watchStatements(t *testing.T, tx db.Tx[db.Tenant], name string) *[]string {
	t.Helper()
	seen := new([]string)
	callbacks := tx.DB().Callback().Query()
	if err := callbacks.After("gorm:query").Register(name, func(g *gorm.DB) {
		*seen = append(*seen, g.Statement.SQL.String())
	}); err != nil {
		t.Fatalf("watch the statements: %v", err)
	}
	t.Cleanup(func() { callbacks.Remove(name) })
	return seen
}

// probeValue is one operand of the kind the named column holds, and the shape an
// operator that takes no operand is written with.
func probeValue(field, op string) any {
	switch op {
	case "is_null", "not_null":
		return nil
	case "in":
		return []any{probeValue(field, "eq")}
	}
	switch field {
	case "count":
		return 1
	case "rate":
		return 1.5
	case "flag":
		return true
	case "when":
		return probeDay.Add(time.Hour)
	case "id":
		return uuid.New()
	}
	return "alpha"
}

// TestEveryOperatorAnswersOnlyTheFieldTypesTheTableAllows asks each column of the
// fixture of each operator the grammar has. The admitted pairs answer, the rest
// refuse with ErrInvalid: the table is the rule, and it holds in both directions.
// An order is asked of a count, an amount or a moment and of nothing else — the same
// rule the mount gate holds for a declared sub-count, so a page cannot narrow by a
// comparison a card is refused for counting by.
func TestEveryOperatorAnswersOnlyTheFieldTypesTheTableAllows(t *testing.T) {
	conn := seedProbes(t)
	// admitted names the operators the table allows each column; "" refuses every
	// one, which is what a list column and a name the schema does not know do.
	admitted := map[string]string{
		"label":  "eq ne in is_null not_null eq_fold",
		"body":   "eq ne in is_null not_null eq_fold",
		"count":  "eq ne in lte lt gt gte is_null not_null",
		"rate":   "eq ne in lte lt gt gte is_null not_null",
		"flag":   "eq ne in is_null not_null",
		"when":   "eq ne in lte lt gt gte is_null not_null",
		"id":     "eq ne in is_null not_null",
		"tags":   "",
		"noSuch": "",
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, field := range slices.Sorted(maps.Keys(admitted)) {
			for _, op := range crud.Ops {
				p := crud.Predicate{Field: field, Op: crud.Op(op), Value: probeValue(field, op)}
				_, err := crud.CountWhere[*Probe](tx, crud.Where{p})
				admits := strings.Contains(" "+admitted[field]+" ", " "+op+" ")
				switch {
				case admits && err != nil:
					t.Errorf("%s %s is refused: %v", field, op, err)
				case !admits && !errors.Is(err, crud.ErrInvalid):
					t.Errorf("%s %s answered %v rather than refusing", field, op, err)
				}
			}
		}
	})
}

// TestEachOperatorCountsExactlyTheRowsItNames pins the number behind each admitted
// pair, because a comparison that answers the wrong set still answers: only the
// count says whether the operator meant what its name says.
func TestEachOperatorCountsExactlyTheRowsItNames(t *testing.T) {
	conn := seedProbes(t)
	moment := func(hour int) time.Time { return probeDay.Add(time.Duration(hour) * time.Hour) }
	cases := []struct {
		name string
		p    crud.Predicate
		want int64
	}{
		{"equality reads case", crud.Predicate{Field: "label", Op: crud.OpEq, Value: "alpha"}, 1},
		{"difference excludes the named value", crud.Predicate{Field: "label", Op: crud.OpNe, Value: "alpha"}, 6},
		{"membership holds its two values", crud.Predicate{Field: "label", Op: crud.OpIn, Value: []string{"alpha", "beta"}}, 2},
		{"membership of numbers", crud.Predicate{Field: "count", Op: crud.OpIn, Value: []int{4, 5, 6, 7}}, 4},
		{"a count below", crud.Predicate{Field: "count", Op: crud.OpLt, Value: 3}, 2},
		{"a count up to", crud.Predicate{Field: "count", Op: crud.OpLte, Value: 3}, 3},
		{"a count above", crud.Predicate{Field: "count", Op: crud.OpGt, Value: 5}, 2},
		{"a count from", crud.Predicate{Field: "count", Op: crud.OpGte, Value: 5}, 3},
		{"an amount above", crud.Predicate{Field: "rate", Op: crud.OpGt, Value: 3.5}, 4},
		{"an amount widened from a count", crud.Predicate{Field: "rate", Op: crud.OpGte, Value: 5}, 3},
		{"a flag by name", crud.Predicate{Field: "flag", Op: crud.OpEq, Value: true}, 4},
		{"a flag that is not the one asked for", crud.Predicate{Field: "flag", Op: crud.OpNe, Value: true}, 3},
		{"a moment from", crud.Predicate{Field: "when", Op: crud.OpGte, Value: moment(24)}, 3},
		{"a moment before", crud.Predicate{Field: "when", Op: crud.OpLt, Value: moment(24)}, 1},
		{"absence", crud.Predicate{Field: "when", Op: crud.OpIsNull}, 3},
		{"presence", crud.Predicate{Field: "when", Op: crud.OpNotNull}, 4},
		{"folded case", crud.Predicate{Field: "label", Op: crud.OpEqFold, Value: "munich"}, 2},
		{"folded case of a paragraph", crud.Predicate{Field: "body", Op: crud.OpEqFold, Value: "FIRST"}, 1},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, tc := range cases {
			got, err := crud.CountWhere[*Probe](tx, crud.Where{tc.p})
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
				continue
			}
			if got != tc.want {
				t.Errorf("%s: %v answers %d of seven rows, want %d", tc.name, tc.p, got, tc.want)
			}
		}
	})
}

// TestAComparisonWithNoValueIsRefusedRatherThanMatchingNothing keeps every
// correctable shape out of the database. `col = NULL` matches nothing, so a filter
// that accepted no value would be the whole set wearing a narrowing; the answer here
// is a sentence, and nothing is sent.
func TestAComparisonWithNoValueIsRefusedRatherThanMatchingNothing(t *testing.T) {
	conn := seedProbes(t)
	refused := crud.Where{
		{Field: "label", Op: crud.OpEq, Value: nil},
		{Field: "label", Op: crud.OpNe, Value: nil},
		{Field: "label", Op: crud.OpEqFold, Value: nil},
		{Field: "count", Op: crud.OpGt, Value: nil},
		{Field: "when", Op: crud.OpLte, Value: nil},
		{Field: "when", Op: crud.OpIn, Value: []any{probeDay, nil}},
		{Field: "label", Op: crud.OpIsNull, Value: "beta"},
		{Field: "when", Op: crud.OpNotNull, Value: 0},
		{Field: "label", Op: crud.OpIn, Value: "alpha"},
		{Field: "label", Op: crud.OpIn, Value: map[string]int{"alpha": 1}},
		{Field: "label", Op: crud.OpIn, Value: []byte("alpha")},
		{Field: "label", Op: crud.OpEq, Value: 7},
		{Field: "count", Op: crud.OpEq, Value: "one"},
		{Field: "count", Op: crud.OpEq, Value: 1.5},
		{Field: "rate", Op: crud.OpGte, Value: "several"},
		{Field: "flag", Op: crud.OpEq, Value: "true"},
		{Field: "when", Op: crud.OpEq, Value: "2026-01-01"},
		{Field: "when", Op: crud.OpGt, Value: time.Now().UTC().Format(time.RFC3339)},
		{Field: "id", Op: crud.OpEq, Value: "not-a-uuid"},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.noValue")
		for _, p := range refused {
			_, err := crud.CountWhere[*Probe](tx, crud.Where{p})
			if !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("%v answered %v, want a refusal naming the operand", p, err)
			}
		}
		if len(*sent) != 0 {
			t.Errorf("a refused question sent %d statements: %v", len(*sent), *sent)
		}
	})
}

// TestANullColumnIsInvisibleToEveryComparisonAndOnlyIsNullSeesIt is SQL's
// three-valued logic, read from the rows rather than from the manual: three of the
// seven probes carry no moment, no comparison of any kind finds them, and is_null is
// the only operator that does. Equality and difference partition the rows that do
// have a value, which is why not_null exists.
func TestANullColumnIsInvisibleToEveryComparisonAndOnlyIsNullSeesIt(t *testing.T) {
	conn := seedProbes(t)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		equal, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "when", Op: crud.OpEq, Value: probeDay.Add(26 * time.Hour)}})
		if err != nil {
			t.Fatalf("count the one moment named: %v", err)
		}
		different, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "when", Op: crud.OpNe, Value: probeDay.Add(26 * time.Hour)}})
		if err != nil {
			t.Fatalf("count the moments that are not it: %v", err)
		}
		present, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "when", Op: crud.OpNotNull}})
		if err != nil {
			t.Fatalf("count the rows with a moment: %v", err)
		}
		if equal != 1 || different != 3 {
			t.Errorf("the moment named answers %d and the rest %d, want 1 and 3 of the four probes that have one", equal, different)
		}
		if equal+different != present {
			t.Errorf("equality %d and difference %d do not partition the %d rows that have a moment", equal, different, present)
		}
		missing, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "when", Op: crud.OpIsNull}})
		if err != nil {
			t.Fatalf("count the rows with no moment: %v", err)
		}
		if missing != 3 {
			t.Errorf("is_null finds %d rows, want the three with no moment", missing)
		}
		// A membership list excludes them too, and an order asked of them answers
		// nothing, which is the same three-valued rule read two more ways.
		anyOfFour := crud.Where{
			{Field: "when", Op: crud.OpIn, Value: []time.Time{probeDay.Add(9 * time.Hour), probeDay.Add(26 * time.Hour), probeDay.Add(50 * time.Hour), probeDay.Add(74 * time.Hour)}},
			{Field: "when", Op: crud.OpGte, Value: probeDay.Add(-24 * time.Hour)},
		}
		for _, p := range anyOfFour {
			got, err := crud.CountWhere[*Probe](tx, crud.Where{p})
			if err != nil {
				t.Errorf("%v: %v", p, err)
				continue
			}
			if got != 4 {
				t.Errorf("%v finds %d rows, want the four that have a moment and not the three that have none", p, got)
			}
		}
	})
}

// TestAnEmptyInMatchesNothingAndIsNeverSpelledAsAnEmptyList checks the one clause
// the grammar could not copy from anywhere: `IN ()` is not SQL and `IN (NULL)` is
// three-valued, so the empty set of allowed values is the false clause, and the
// total, the page and the text of the statement all say the same thing.
func TestAnEmptyInMatchesNothingAndIsNeverSpelledAsAnEmptyList(t *testing.T) {
	conn := seedProbes(t)
	empties := []crud.Where{
		{{Field: "label", Op: crud.OpIn, Value: nil}},
		{{Field: "label", Op: crud.OpIn, Value: []string{}}},
		{{Field: "label", Op: crud.OpIn, Value: []any{}}},
		{{Field: "count", Op: crud.OpIn, Value: []int64(nil)}},
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.emptyIn")
		for _, w := range empties {
			total, err := crud.CountWhere[*Probe](tx, w)
			if err != nil {
				t.Errorf("%v: %v", w, err)
				continue
			}
			if total != 0 {
				t.Errorf("%v answers %d rows, want none", w, total)
			}
			rows, pageTotal, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Where: w, Limit: 10})
			if err != nil {
				t.Errorf("%v as a page: %v", w, err)
				continue
			}
			if len(rows) != 0 || pageTotal != 0 {
				t.Errorf("%v as a page answers %d of %d, want none of none", w, len(rows), pageTotal)
			}
		}
		for _, sql := range *sent {
			lower := strings.ToLower(sql)
			if strings.Contains(lower, "in ()") || strings.Contains(lower, "in (null)") {
				t.Errorf("an empty list reached the wire as %q", sql)
			}
		}
		// The question either side of the empty one is still answered: the false
		// clause narrowed one read and poisoned nothing.
		all, err := crud.CountWhere[*Probe](tx, crud.Where{{Field: "label", Op: crud.OpNotNull}})
		if err != nil || all != 7 {
			t.Errorf("the same transaction answers %d rows (%v), want the seven probes", all, err)
		}
	})
}

// TestAnInOfMoreValuesThanMaxInValuesIsRefusedBeforeAnythingIsSent asks the bound at
// its edge: a page's worth of values is a question, one more is a caller that has
// lost the page, and the refusal is written before a statement exists.
func TestAnInOfMoreValuesThanMaxInValuesIsRefusedBeforeAnythingIsSent(t *testing.T) {
	conn := seedProbes(t)
	ids := make([]uuid.UUID, crud.MaxInValues+1)
	for i := range ids {
		ids[i] = uuid.New()
	}
	at := func(n int) crud.Where {
		return crud.Where{{Field: "id", Op: crud.OpIn, Value: ids[:n]}}
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.inBound")
		if _, err := crud.CountWhere[*Probe](tx, at(crud.MaxInValues)); err != nil {
			t.Errorf("a page's worth of %d values is refused: %v", crud.MaxInValues, err)
		}
		_, err := crud.CountWhere[*Probe](tx, at(crud.MaxInValues+1))
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("%d values answered %v, want a refusal", crud.MaxInValues+1, err)
		}
		said := err.Error()
		if !strings.Contains(said, strconv.Itoa(crud.MaxInValues+1)) || !strings.Contains(said, strconv.Itoa(crud.MaxInValues)) {
			t.Errorf("the refusal %q names neither the count it was given nor the ceiling", said)
		}
		if len(*sent) != 1 {
			t.Errorf("the over-long list sent %d statements: %v", len(*sent), *sent)
		}
	})
}

// TestQueryOperandsPastMaxQueryValuesAreRefused asks the whole-question bound, and
// asks it of both doors: a list too wide for a page is too wide for the card beside
// it, because both are the same question built by the same builder.
func TestQueryOperandsPastMaxQueryValuesAreRefused(t *testing.T) {
	conn := seedProbes(t)
	ids := make([]uuid.UUID, crud.MaxInValues)
	for i := range ids {
		ids[i] = uuid.New()
	}
	one := crud.Predicate{Field: "id", Op: crud.OpIn, Value: ids}
	inside, over := crud.Where{}, crud.Where{}
	for range crud.MaxQueryValues / crud.MaxInValues {
		inside = append(inside, one)
		over = append(over, one)
	}
	over = append(over, crud.Predicate{Field: "label", Op: crud.OpEq, Value: "alpha"})
	if len(over) != len(inside)+1 {
		t.Fatalf("the two questions are not one predicate apart")
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		if _, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Where: inside, Limit: 10}); err != nil {
			t.Errorf("%d values are refused: %v", crud.MaxQueryValues, err)
		}
		_, err := crud.CountWhere[*Probe](tx, over)
		if !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("%d values answered %v, want a refusal", crud.MaxQueryValues+1, err)
		}
		if !strings.Contains(err.Error(), strconv.Itoa(crud.MaxQueryValues)) {
			t.Errorf("the refusal %q does not name the ceiling", err)
		}
		if _, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Where: over, Limit: 10}); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("the page accepted what the total refused: %v", err)
		}
	})
}

// TestAPredicateNamingAColumnRatherThanAFieldIsRefused is the injection case. Every
// name reaches a statement through the schema and nothing else, so the only text a
// caller can put in the column position is a text the schema refused first — and the
// lifecycle and tenant columns are not names in any schema, so no predicate can reach
// them either.
func TestAPredicateNamingAColumnRatherThanAFieldIsRefused(t *testing.T) {
	conn := seedProbes(t)
	names := []string{
		"label; DROP TABLE crud_probes",
		"due_at",
		"1=1 or 1=1",
		"label --",
		"label) = 'x' or (1=1",
		"deletedAt",
		"deleted_at",
		"tenantId",
		"tenant_id",
		"_i18n",
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		sent := watchStatements(t, tx, "pkit.crudtest.names")
		for _, name := range names {
			for _, op := range crud.Ops {
				p := crud.Predicate{Field: name, Op: crud.Op(op), Value: probeValue(name, op)}
				if _, err := crud.CountWhere[*Probe](tx, crud.Where{p}); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("%s %s answered %v, want a refusal that it names no field", name, op, err)
				}
				if _, _, err := crud.ListCtx[*Probe](ctx, tx, crud.Query{Where: crud.Where{p}}); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("%s %s as a page answered %v", name, op, err)
				}
			}
		}
		if len(*sent) != 0 {
			t.Fatalf("a name the schema does not know sent %d statements: %v", len(*sent), *sent)
		}
		// The table is still there and still whole, which is what a statement that
		// was never sent is supposed to leave behind.
		standing, err := crud.CountWhere[*Probe](tx, nil)
		if err != nil || standing != int64(len(probeRows)) {
			t.Errorf("%d probes stand after the refusals (%v), want %d", standing, err, len(probeRows))
		}
	})
}

// TestAQueryWithTwoUnanswerableNamesRefusesWithTheSameSentenceEveryTime pins the one
// thing a map walk cannot promise: which of two bad names a refusal names. The answer
// of an answerable question never moves, because AND is commutative, so fixing the
// walk is only about which sentence a caller reads twice.
func TestAQueryWithTwoUnanswerableNamesRefusesWithTheSameSentenceEveryTime(t *testing.T) {
	conn := seedProbes(t)
	q := crud.Query{
		Filter: map[string]any{"zebraColumn": "x", "alphaColumn": "y"},
		Where:  crud.Where{{Field: "middleColumn", Op: crud.OpEq, Value: "z"}},
	}
	var said string
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for range 20 {
			_, _, err := crud.ListCtx[*Probe](ctx, tx, q)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("the query answered %v, want a refusal", err)
			}
			if said == "" {
				said = err.Error()
			} else if err.Error() != said {
				t.Fatalf("the same query refused twice two ways: %q and %q", said, err.Error())
			}
		}
	})
	if !strings.Contains(said, "alphaColumn") {
		t.Errorf("the refusal %q names neither of the two bad names in field order", said)
	}
}
