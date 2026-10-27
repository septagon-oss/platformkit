package crud

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// predicate.go is the comparison grammar a stored question is spelled with, and
// the two reads that ask it: a total narrowed to one predicate set, and one
// timestamp column broken into days.
//
// The grammar is data and never a fragment of SQL. A column name reaches a
// statement only through the schema — the same route an equality filter already
// takes — so a question written by one module can name no column of another, and
// no caller of this package holds a way to write one.
//
// Query.Where carries this grammar to a page, and CountWhere carries it to a total
// with no page. Neither is a public parameter: the list grammar
// (?filter=field:value) stays equality-only, because a new meaning inside the
// existing parameter would change what the documents quoting it have always meant.

// Op is one comparison against a field's own value.
type Op string

// The comparisons a question can ask. Each is checked against the type of the
// field it names, so an operator a column cannot answer is refused rather than
// spelled as SQL that would match nothing. A containment test is absent because
// nothing declares one yet, and an operator no declaration uses is an operator no
// mount gate has ever had to refuse.
const (
	OpEq Op = "eq"

	// OpNe is `col <> ?`. A row whose column is NULL answers no comparison, so it
	// is not on a `ne` page either; OpNotNull is how a caller says it means those
	// rows too.
	OpNe Op = "ne"

	// OpIn is `col IN (?,?,…)` with one placeholder per value. An empty list is
	// the false clause `1 = 0` and never `IN ()`, which is not SQL.
	OpIn Op = "in"

	// OpIsNull and OpNotNull are the only way to see a NULL column, and take no
	// operand. Whether a column may be NULL is no fact the schema records, so an
	// is_null asked of a NOT NULL column answers zero rows, which is an honest
	// answer to a question nobody could ask better.
	OpIsNull  Op = "is_null"
	OpNotNull Op = "not_null"

	// OpLt, OpLte, OpGt and OpGte ask where a value sits on a scale, which only a
	// count, an amount or a moment has.
	OpLt  Op = "lt"
	OpLte Op = "lte"
	OpGt  Op = "gt"
	OpGte Op = "gte"

	// OpEqFold is `lower(col COLLATE "C") = lower((?)::text COLLATE "C")`: it
	// folds ASCII A-Z onto a-z and nothing else, alike in every installation
	// whatever the cluster's locale, because an uncollated lower() means whatever
	// that libc means and folds lossily. The stated price: no Unicode or accent
	// folding, and no index until a deployment creates the matching expression.
	OpEqFold Op = "eq_fold"
)

// Ops is every operator a predicate may carry. The list is here rather than only
// in the constants because the mount gate refuses a name outside it and has to
// say what it admits. What a *declared* sub-count may ask with is narrower than
// what this grammar can spell, and kit/rest owns that shorter list.
var Ops = []string{
	string(OpEq), string(OpEqFold),
	string(OpLt), string(OpLte), string(OpGt), string(OpGte),
	string(OpNe), string(OpIn), string(OpIsNull), string(OpNotNull),
}

// ValidOp reports whether a declaration names a comparison this grammar has.
func ValidOp(name string) bool {
	for _, op := range Ops {
		if op == name {
			return true
		}
	}
	return false
}

// symbol is the SQL each comparison spells. The membership and nullness operators
// are spelled by their own builders below, each of which takes an operand shape the
// comparisons do not.
var symbol = map[Op]string{
	OpEq: "=", OpNe: "<>", OpLt: "<", OpLte: "<=", OpGt: ">", OpGte: ">=",
}

// orderOps is the four comparisons that ask where a value sits on a scale.
var orderOps = map[Op]bool{OpLt: true, OpLte: true, OpGt: true, OpGte: true}

// Predicate is one comparison on one field of this entity. Value is the value
// already read as the field's own type — a time.Time for a timestamp, an int64
// for a count — because the coercion belongs to whoever checked the declaration,
// not to the statement.
//
// The operand's Go type has to be the column's own. A count takes any integer kind
// and an amount any float or integer kind, because those are widths of one number
// and the wider reading of it is exact; a word, a flag, a moment and an id are read
// only as string, bool, time.Time and uuid.UUID, so a named type over one of them —
// a `type Status string` — is refused rather than reinterpreted. The refusal names
// the operand's type and sends no statement: the conversion is the caller's, and
// nothing here guesses at a spelling of a value.
type Predicate struct {
	Field string
	Op    Op

	// Value is the operand: none at all for is_null and not_null, a slice or array
	// for in, and the column's own type for every other operator.
	Value any
}

// Where is a question: every predicate must hold, the same way every key of a
// filter map must hold for one row to be on the page. The conjunction has no OR:
// a caller that wants a union of two sets asks twice, and what one question cannot
// answer is one total over a union.
type Where []Predicate

// fragment is one WHERE condition and the values it takes. The two travel
// together because a comparison takes one value and a membership test takes as
// many as it names. Pairing clause i with argument i instead breaks the question
// where the first list sits — one placeholder sent against a clause that lists
// several is a syntax error Postgres refuses — and panics before that on a clause
// with no operand at all, the empty list's `1 = 0`. Both statements come from one
// list of these, so the failure lands on the total and the page together, loudly
// and never as two sets that disagree.
type fragment struct {
	sql  string
	args []any
}

// predicateSQL spells one predicate as one condition and its values, with the
// column taken from the schema and every value left as a parameter. An unknown
// name, a list column and an unknown operator are refused rather than dropped: a
// narrowed count that quietly lost its predicate is the unfiltered total wearing a
// label.
func predicateSQL(fields []Field, p Predicate) (fragment, error) {
	f, err := comparable(fields, p.Field, "filter")
	if err != nil {
		return fragment{}, err
	}
	switch p.Op {
	case OpIsNull, OpNotNull:
		return nullarySQL(f, p)
	case OpIn:
		return inSQL(f, p)
	case OpEqFold:
		return foldSQL(f, p)
	}
	sym, ok := symbol[p.Op]
	if !ok {
		return fragment{}, fmt.Errorf("%w: %q is not an operator to filter %s on; Ops admits %s",
			ErrInvalid, string(p.Op), p.Field, strings.Join(Ops, ", "))
	}
	if err := orderFault(f, p.Op); err != nil {
		return fragment{}, err
	}
	if err := operandFault(f, p.Op, p.Value); err != nil {
		return fragment{}, err
	}
	return fragment{sql: f.Column + " " + sym + " ?", args: []any{p.Value}}, nil
}

// nullarySQL is is_null and not_null: the column, one of two spellings, and no
// operand at all. An operator that quietly ignored a value would teach the reader
// that the value was doing nothing, which is exactly what it was doing.
func nullarySQL(f Field, p Predicate) (fragment, error) {
	if p.Value != nil {
		return fragment{}, fmt.Errorf("%w: %s %s takes no value", ErrInvalid, f.Name, p.Op)
	}
	if p.Op == OpIsNull {
		return fragment{sql: f.Column + " IS NULL"}, nil
	}
	return fragment{sql: f.Column + " IS NOT NULL"}, nil
}

// inSQL is the membership test, spelled with one placeholder per value: the number
// of values a caller listed has to be the number of parameters the statement really
// carries, because MaxInValues bounds the work and not the text.
//
// An empty list is `1 = 0`. `IN ()` is not SQL and `IN (NULL)` is three-valued;
// both would answer nothing by accident, and an empty set of allowed values answers
// nothing because that is what an empty set of allowed values means.
func inSQL(f Field, p Predicate) (fragment, error) {
	list, ok := listValues(p.Value)
	if !ok {
		return fragment{}, fmt.Errorf("%w: %s %s takes a list of values, not a %T", ErrInvalid, f.Name, p.Op, p.Value)
	}
	if len(list) == 0 {
		return fragment{sql: "1 = 0"}, nil
	}
	if len(list) > MaxInValues {
		return fragment{}, fmt.Errorf("%w: %s %s holds %d values; MaxInValues is %d",
			ErrInvalid, f.Name, p.Op, len(list), MaxInValues)
	}
	marks := make([]string, len(list))
	for i, v := range list {
		if err := operandFault(f, p.Op, v); err != nil {
			return fragment{}, err
		}
		marks[i] = "?"
	}
	return fragment{sql: f.Column + " IN (" + strings.Join(marks, ", ") + ")", args: list}, nil
}

// foldSQL is case-folded equality on a word. The COLLATE "C" is the whole rule:
// without it the fold means whatever this cluster's libc means, so one record
// answers a different set of rows in another locale, and the fold there is lossy.
// The parameter is cast before it collates because a bare ? reaches Postgres with
// no type and COLLATE on an unknown type is refused. The price is the one stated on
// OpEqFold: ASCII only, and no index until a deployment creates the matching one.
func foldSQL(f Field, p Predicate) (fragment, error) {
	if f.Type != TypeString && f.Type != TypeText {
		return fragment{}, fmt.Errorf("%w: %s is a %s, and folding case reads a word", ErrInvalid, f.Name, f.Type)
	}
	if err := operandFault(f, OpEqFold, p.Value); err != nil {
		return fragment{}, err
	}
	return fragment{
		sql:  `lower(` + f.Column + ` COLLATE "C") = lower((?)::text COLLATE "C")`,
		args: []any{p.Value},
	}, nil
}

// orderFault is this package's half of the rule the mount gate already holds for a
// declared sub-count: only a count, an amount or a moment is asked about with an
// order. Stating it where the statement is built is what keeps a page from narrowing
// by a comparison a card is refused for counting by — and keeps a text column's
// collation from deciding which rows a range holds.
func orderFault(f Field, op Op) error {
	if !orderOps[op] {
		return nil
	}
	switch f.Type {
	case TypeInt, TypeFloat, TypeTime:
		return nil
	}
	return fmt.Errorf("%w: %s %s compares a %s, and only a count, an amount or a moment is asked about with an order",
		ErrInvalid, f.Name, op, f.Type)
}

// operandFault refuses a value the column cannot hold, including no value at all:
// `col = NULL` matches nothing, which is the shape of a filter that quietly does
// nothing rather than the refusal it has to be. The check is on the Go shape and not
// on text, because reading text as the field's own type is Coerce's door and a
// statement that guessed at a spelling would compare a value nobody wrote. The
// sentence names the field, the operator and the operand's Go type, never the
// operand: a predicate's value is a record's own data, and an error log is an export.
//
// Kind is checked and range is not: how wide a column is belongs to the migration that
// made it, and this package reads no schema. An admitted kind too wide for this column
// is refused by the driver before the statement goes out, and Classify passes that on
// unclassified — a logged failure and not an ErrInvalid. Refusing the number itself
// would take the width as a fact a field carries, and no field carries one today.
func operandFault(f Field, op Op, v any) error {
	if v == nil {
		return fmt.Errorf("%w: %s %s carries no value; is_null asks about absence", ErrInvalid, f.Name, op)
	}
	if holds(f, v) {
		return nil
	}
	return fmt.Errorf("%w: %s is a %s and the %s operand is a %T", ErrInvalid, f.Name, f.Type, op, v)
}

// holds is whether one Go value can be sent as this column's own type: its kind, and
// not its range. An integer column takes any Go integer kind and a float column takes
// those and the float kinds, because they are widths of one number — one read as the
// other is exact up to 2^53 and rounds above it. A word, a flag, a moment and an id
// are read by type and not by kind — the kind of a moment is a struct and of an id an
// array, and neither says what it holds — so a named type over either is refused
// rather than converted. Predicate states the rule where whoever writes an operand
// reads it, and operandFault says what this package leaves the driver to refuse.
func holds(f Field, v any) bool {
	switch f.Type {
	case TypeString, TypeText:
		_, ok := v.(string)
		return ok
	case TypeInt:
		return integerKind(v)
	case TypeFloat:
		return floatKind(v) || integerKind(v)
	case TypeBool:
		_, ok := v.(bool)
		return ok
	case TypeTime:
		_, ok := v.(time.Time)
		return ok
	case TypeUUID:
		_, ok := v.(uuid.UUID)
		return ok
	}
	return false
}

func integerKind(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	}
	return false
}

func floatKind(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// listValues is a slice or array as its own elements, and a nil operand as the empty
// list it is: an In that carries nothing narrows to nothing, which is the honest
// direction for a filter with no values to compare against.
func listValues(v any) ([]any, bool) {
	if v == nil {
		return nil, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// fragments is the question one read asks: every equality key of a filter map and
// every predicate, ANDed. The lifecycle clause belongs to the door that asks —
// deleted_at IS NULL is added there and no predicate can name it, because deletedAt
// is no field of any schema.
//
// Filter keys come first in sorted field order and predicates follow as written, so
// a question unanswerable two ways refuses with one sentence every time. A field
// named twice is narrowed twice, never overwritten: the AND is what the caller wrote.
//
// Both doors build through here — CountWhere, and the count and the page of ListCtx —
// so a narrowed total and a narrowed page answer one set and MaxQueryValues binds at
// both doors at once. Nothing else in this package turns a caller's condition into SQL:
// the lifecycle clause names no field of any schema, and CountDays opens and closes
// its window on the one timestamp column it was handed.
func fragments(fields []Field, filter map[string]any, w Where) ([]fragment, error) {
	asked := make(Where, 0, len(filter)+len(w))
	for _, name := range slices.Sorted(maps.Keys(filter)) {
		asked = append(asked, Predicate{Field: name, Op: OpEq, Value: filter[name]})
	}
	asked = append(asked, w...)
	out := make([]fragment, 0, len(asked))
	values := 0
	for _, p := range asked {
		c, err := predicateSQL(fields, p)
		if err != nil {
			return nil, err
		}
		values += len(c.args)
		out = append(out, c)
	}
	if values > MaxQueryValues {
		return nil, fmt.Errorf("%w: this question carries %d values; MaxQueryValues is %d",
			ErrInvalid, values, MaxQueryValues)
	}
	return out, nil
}

// Coerce reads a filter value as the field's own type. A field the grammar reads
// as a string takes the text; a number, a moment, a uuid and a flag are refused
// with ErrInvalid when the text is not one, because a filter that silently
// compared nothing would answer with the whole set. The table is entity.Coerce,
// which a declared default and a deck's swipe argument read too.
func Coerce(f Field, raw string) (any, error) {
	v, err := entity.Coerce(f, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return v, nil
}

// DayStart is the instant that opens the UTC day the time falls in.
func DayStart(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// Declared reads one string written in a declaration as the field's own type,
// against the one instant the answer is about. A timestamp field may name an
// entity.Anchors word — "today" is the day the person was asked about, not the day
// the binary was built — and anything else is a literal Coerce reads.
func Declared(f Field, raw string, asOf time.Time) (any, error) {
	if f.Type == TypeTime && entity.ValidAnchor(raw) {
		switch raw {
		case "now":
			return asOf, nil
		case "today":
			return DayStart(asOf), nil
		default: // "tomorrow", the one name left in the vocabulary
			return DayStart(asOf).Add(24 * time.Hour), nil
		}
	}
	return Coerce(f, raw)
}

// Resolve turns the predicates a declaration spells into the values a statement
// takes, read at one instant. It is the only path from a declared string to a
// query parameter, and it refuses rather than dropping one condition: a narrowed
// total that silently lost its predicate is the unfiltered total.
func Resolve(fields []Field, named []entity.EntryPredicate, asOf time.Time) (Where, error) {
	out := make(Where, 0, len(named))
	for _, d := range named {
		f, ok := FieldNamed(fields, d.Field)
		if !ok {
			return nil, fmt.Errorf("%w: there is no field %q to filter on", ErrInvalid, d.Field)
		}
		v, err := Declared(f, d.Value, asOf)
		if err != nil {
			return nil, err
		}
		out = append(out, Predicate{Field: d.Field, Op: Op(d.Op), Value: v})
	}
	return out, nil
}

// CountWhere counts this tenant's live rows the predicates admit. It applies the
// same lifecycle predicate Count does, so a narrowed total and the whole one are
// answers about the same set of rows, and it builds its conditions through the same
// fragments the page is built from, so the two cannot disagree about the question.
//
// It takes predicates and no equality map: a caller holding a map writes its keys as
// OpEq predicates, which is exactly the walk fragments does for a page's Filter, so
// the two forms reach the same conditions and one door stays as narrow as the
// question it answers.
func CountWhere[T Entity](tx db.Tx[db.Tenant], w Where) (int64, error) {
	clauses, err := fragments(Fields[T](), nil, w)
	if err != nil {
		return 0, err
	}
	query := tx.DB().Model(blank[T]()).Where("deleted_at IS NULL")
	for _, c := range clauses {
		query = query.Where(c.sql, c.args...)
	}
	var total int64
	return total, Classify(query.Count(&total).Error)
}

// DayCount is one UTC calendar day and how many rows fall in it. Day is the
// instant that opens the day.
type DayCount struct {
	Day   time.Time
	Count int64
}

// CountDays breaks this tenant's live rows into the UTC days of one timestamp
// column inside [from, to). It names only the days it answered: turning a day the
// query did not name into a zero is the reader's.
//
// A window with no rows in it answers an empty slice, never nil: nil is the shape
// of a read that never happened, and a reader that has to tell "nothing in the
// window" apart from "nobody looked" is a reader drawing a gap where the answer
// was "none".
//
// The truncation is `AT TIME ZONE 'UTC'` and not a bare date_trunc: on a
// timestamptz, date_trunc truncates in the session's own zone, and nothing here
// sets one, so a server configured elsewhere would move every bucket by its offset.
func CountDays[T Entity](tx db.Tx[db.Tenant], field string, from, to time.Time) ([]DayCount, error) {
	f, err := comparable(Fields[T](), field, "group on")
	if err != nil {
		return nil, err
	}
	if f.Type != TypeTime {
		return nil, fmt.Errorf("%w: %s is a %s, and a day breakdown reads a column of days", ErrInvalid, field, f.Type)
	}
	// The whole expression, and not the ordinal: gorm quotes what it is handed, so
	// `GROUP BY 1` arrives as `GROUP BY "1"` and is refused as a column that does
	// not exist.
	day := "date_trunc('day', " + f.Column + " AT TIME ZONE 'UTC')"
	var rows []DayCount
	err = tx.DB().Model(blank[T]()).
		Select(day+" AS day, count(*) AS count").
		Where("deleted_at IS NULL AND "+f.Column+" >= ? AND "+f.Column+" < ?", from, to).
		Group(day).Order(day).Scan(&rows).Error
	if err != nil {
		return nil, Classify(err)
	}
	if rows == nil {
		// gorm's Scan leaves the destination untouched when the query answered no
		// row at all, so an empty window would otherwise come back in the shape of
		// a read that never ran.
		rows = []DayCount{}
	}
	for i, row := range rows {
		rows[i].Day = row.Day.UTC()
	}
	return rows, nil
}
