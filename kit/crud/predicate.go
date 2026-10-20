package crud

import (
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// predicate.go is the comparison grammar a stored question is spelled with, and
// the two reads that ask it: a total narrowed to one predicate set, and one
// timestamp column broken into days.
//
// The grammar is data and never a fragment of SQL. A column name reaches a
// statement only through the schema — the same route conditions already takes for
// an equality filter — so a question written by one module can name no column of
// another, and no caller of this package holds a way to write one.
//
// The list grammar (?filter=field:value) stays equality-only: this is the
// internal form a checked declaration builds, not a public parameter, and a new
// meaning inside the existing parameter would change what the documents quoting
// it have always meant.

// Op is one comparison against a field's own value.
type Op string

// The five comparisons. `ne`, `in` and a containment test are absent because
// nothing declares them yet, and an operator no declaration uses is an operator
// no mount gate has ever had to refuse.
const (
	OpEq  Op = "eq"
	OpLt  Op = "lt"
	OpLte Op = "lte"
	OpGt  Op = "gt"
	OpGte Op = "gte"
)

// Ops is every operator a predicate may carry. The list is here rather than only
// in the constants because the mount gate refuses a name outside it and has to
// say what it admits.
var Ops = []string{string(OpEq), string(OpLt), string(OpLte), string(OpGt), string(OpGte)}

// ValidOp reports whether a declaration names a comparison this grammar has.
func ValidOp(name string) bool {
	for _, op := range Ops {
		if op == name {
			return true
		}
	}
	return false
}

// symbol is the SQL each operator spells.
var symbol = map[Op]string{
	OpEq: "=", OpLt: "<", OpLte: "<=", OpGt: ">", OpGte: ">=",
}

// Predicate is one comparison on one field of this entity. Value is the value
// already read as the field's own type — a time.Time for a timestamp, an int64
// for a count — because the coercion belongs to whoever checked the declaration,
// not to the statement.
type Predicate struct {
	Field string
	Op    Op
	Value any
}

// Where is a question: every predicate must hold, the same way every key of a
// filter map must hold for one row to be on the page.
type Where []Predicate

// predicateSQL turns one predicate into SQL, with the column taken from the schema. An
// unknown name, a list column and an unknown operator are refused rather than
// dropped: a narrowed count that quietly lost its predicate is the unfiltered
// total wearing a label.
func predicateSQL(fields []Field, p Predicate) (string, []any, error) {
	f, err := comparable(fields, p.Field, "filter")
	if err != nil {
		return "", nil, err
	}
	op, ok := symbol[p.Op]
	if !ok {
		return "", nil, fmt.Errorf("%w: %q is not an operator to filter %s on; Ops admits %s",
			ErrInvalid, string(p.Op), p.Field, strings.Join(Ops, ", "))
	}
	return f.Column + " " + op + " ?", []any{p.Value}, nil
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
// answers about the same set of rows.
func CountWhere[T Entity](tx db.Tx[db.Tenant], w Where) (int64, error) {
	fields := Fields[T]()
	query := tx.DB().Model(blank[T]()).Where("deleted_at IS NULL")
	for _, p := range w {
		sql, args, err := predicateSQL(fields, p)
		if err != nil {
			return 0, err
		}
		query = query.Where(sql, args...)
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
