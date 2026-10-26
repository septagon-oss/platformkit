package personaldata

// Erasure and residue checks share one matcher and run in the caller's
// transaction. Postgres RLS supplies the tenant boundary.

import (
	"context"
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
)

// Scrub replaces known identifiers in text columns and returns affected rows.
// Matching is case-insensitive with word boundaries; bare handles match only rows
// carrying the subject's ID. where is optional SQL with ? placeholders for args.
// No identifiers or columns is a no-op. Bounded columns use narrower placeholders
// or trim safely at identifier boundaries; SQL failures return to the caller's transaction.
func Scrub(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return scrub(ctx, tx, s, guard{}, table, columns, false, where, args)
}

// ScrubTouched performs Scrub and sets stamp to db.Now() in
// the same statement. stamp must be a valid column name; advancing it invalidates
// edits based on the row's earlier timestamp.
func ScrubTouched(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table, stamp string,
	columns []string, where string, args ...any) (int64, error) {
	if !namePattern.MatchString(stamp) {
		return 0, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, stamp)
	}
	return scrub(ctx, tx, s, guard{stamp: stamp, at: db.Now()}, table, columns, false, where, args)
}

// ScrubCounted performs Scrub and increments the caller-named revision column
// in the same statement, invalidating edits based on pre-erasure state.
// An invalid counter name is refused before executing the rewrite.
func ScrubCounted(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table, counter string,
	columns []string, where string, args ...any) (int64, error) {
	if !namePattern.MatchString(counter) {
		return 0, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, counter)
	}
	return scrub(ctx, tx, s, guard{counter: counter}, table, columns, false, where, args)
}

// ScrubJSON applies Scrub to serialized jsonb, including escaped identifiers,
// then casts the result back to jsonb. A failed cast refuses the statement.
func ScrubJSON(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return scrub(ctx, tx, s, guard{}, table, columns, true, where, args)
}

// Residue counts rows still matching the subject in the named text columns,
// using Scrub's predicate and the caller's transaction and optional filter.
func Residue(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return rows(ctx, tx, s, table, columns, false, where, args)
}

// ResidueJSON is Residue for a jsonb column.
func ResidueJSON(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return rows(ctx, tx, s, table, columns, true, where, args)
}

// Blank empties matching text columns using Scrub's predicate and returns
// affected rows. Use it for unique identifiers whose schema permits empty values;
// constraints still apply. No identifiers or columns is a no-op. where and args
// have the same meaning as in Scrub, and SQL errors leave rollback to the caller.
func Blank(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	q, err := blanking(s, table, columns, where, args...)
	if err != nil {
		return 0, err
	}
	if q == nil {
		return 0, nil
	}
	sql := "UPDATE " + table + " SET " + strings.Join(q.sets, ", ")
	if q.filter != "" {
		sql += " WHERE " + q.filter
	}
	res := tx.DB().WithContext(ctx).Exec(sql, q.args...)
	return res.RowsAffected, res.Error
}

func scrub(ctx context.Context, tx db.Tx[db.Tenant], s Subject, g guard, table string,
	columns []string, isJSON bool, where string, args []any) (int64, error) {
	rooms, err := bounds(ctx, tx, table, columns)
	if err != nil {
		return 0, err
	}
	q, err := query(s, g, table, columns, isJSON, rooms, where, args)
	if err != nil {
		return 0, err
	}
	if q == nil {
		return 0, nil
	}
	sql := "UPDATE " + table + " SET " + strings.Join(q.sets, ", ")
	if q.filter != "" {
		sql += " WHERE " + q.filter
	}
	res := tx.DB().WithContext(ctx).Exec(sql, q.args...)
	return res.RowsAffected, res.Error
}

func rows(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, isJSON bool, where string, args []any) (int64, error) {
	// No bounds and no guard: a count writes nothing, so it has nothing to keep inside
	// a column's width and nothing to advance. The filter it asks with is the one the
	// rewrite uses, which is the whole point of the re-read.
	if err := writableNames(table, columns); err != nil {
		return 0, err
	}
	ids := matches(s, isJSON)
	if len(ids) == 0 || len(columns) == 0 {
		return 0, nil
	}
	filter, filterArgs, err := scoped(s, table, columns, ids, isJSON, where, args)
	if err != nil {
		return 0, err
	}
	sql := "SELECT count(*) FROM " + table
	if filter != "" {
		sql += " WHERE " + filter
	}
	var n int64
	if err := tx.DB().WithContext(ctx).Raw(sql, filterArgs...).Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
