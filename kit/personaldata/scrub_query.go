package personaldata

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// namePattern is what a table or column name is allowed to look like. A contributor
// passes names out of its own source and never out of a request, so this is not a
// defence against injection: it is the reason these functions may write a name into
// SQL at all, and it turns a mistyped claim into a refusal at the call instead of a
// syntax error from the server.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// blanking builds the statement Blank runs: Scrub's own filter, with a SET list that
// binds nothing. It is a function apart so the shape can be read and tested without a
// server, which is how Scrub's rewrite is read too.
func blanking(s Subject, table string, columns []string, where string, args ...any) (*rewrite, error) {
	if err := writableNames(table, columns); err != nil {
		return nil, err
	}
	ids := matches(s, false)
	if len(ids) == 0 || len(columns) == 0 {
		return nil, nil
	}
	filter, filterArgs, err := scoped(s, table, columns, ids, false, where, args)
	if err != nil {
		return nil, err
	}
	sets := make([]string, 0, len(columns))
	for _, c := range columns {
		sets = append(sets, c+" = ''")
	}
	// No SET expression binds anything, so the statement's whole argument list is the
	// filter's: the caller's own values, then this file's terms, which is the order the
	// finished SQL asks for them in.
	return &rewrite{sets: sets, filter: filter, args: filterArgs}, nil
}

// rewrite is one built question: the SET expressions, the filter that says a row
// names the subject, and the arguments in the order their `?` appear in it.
//
// It is called a rewrite and not a statement because schemaread.go already uses that
// word for one SQL statement of a migration file.
type rewrite struct {
	sets   []string
	filter string
	args   []any
}

// ownRow is the SQL that says a row carries this person's own id in one of its own columns.
//
// The row is named by the table — which namePattern has already refused to build anything
// but a plain identifier out of — and the id is bound rather than written. Reading the row
// as jsonb pairs is what keeps the rule one sentence rather than a list of columns: a uuid
// column and a text column that holds the id read the same way, and a contributor adds a
// person column without editing this file. A row that merely quotes the id inside a
// sentence counts too, which widens the scrub by one handle in a row that does name the
// person; that is the direction this rule is written to fail in.
func ownRow(table string) string {
	return "EXISTS (SELECT 1 FROM jsonb_each_text(to_jsonb(" + table +
		")) AS named WHERE named.value = ?::text)"
}

// guard is the column a row carries so a reader can tell whether the row moved under
// them, and what this rewrite does to it. A row counts its writes (tasks, change proposals,
// translations), or it names the instant it last changed (files), or it carries nothing —
// and the third case is the one where a caller who read the row before the erasure cannot
// be refused, which is why the first two name their column out loud. See ScrubCounted and
// ScrubTouched for the two shapes and for the reason they are not one flag.
type guard struct {
	// counter is a write count to add one to.
	counter string

	// stamp is an instant to move to at.
	stamp string

	// at is the instant written into stamp, read from the kernel's clock by the caller so
	// that the row disagrees with a form served beforehand by exactly this write.
	at time.Time
}

// piece is one fragment of a SET expression and the values its `?` stand for, kept
// together so the fragments of one expression can be put together in the order that reads
// as English and still bind in the order the server asks for them. The wide and narrow
// rewrites of one column are each written once and referred to by name after that; a
// placeholder counted by hand is a value bound one expression away from the pattern that
// means it.
type piece struct {
	sql  string
	args []any
}

// bound puts pieces into one expression, with their arguments in the same order.
func bound(parts ...piece) piece {
	sql := make([]string, 0, len(parts))
	out := piece{}
	for _, p := range parts {
		sql = append(sql, p.sql)
		out.args = append(out.args, p.args...)
	}
	out.sql = strings.Join(sql, "")
	return out
}

// query writes the rewrite, and refuses one whose caller mis-counted its own filter.
//
// A nil statement with no error is the honest no-op: a person with no email, no name
// and no handle has no string to hunt, and a table with no text columns to look in has
// nowhere to hunt — rewriting everything, or nothing, would both be a lie.
func query(s Subject, g guard, table string, columns []string, isJSON bool, rooms map[string]extent,
	where string, args []any) (*rewrite, error) {
	if err := writableNames(table, columns); err != nil {
		return nil, err
	}
	ids := matches(s, isJSON)
	if len(ids) == 0 || len(columns) == 0 {
		return nil, nil
	}
	own := ownRow(table)

	// Every value is bound at the place it is written, in the order the finished SQL
	// asks for it, and a value used twice is bound twice. Neither this file nor a
	// caller ever counts a placeholder, which is why the caller's own arguments go
	// last: their `?` sit in the filter, written after the SET expressions.
	q := &rewrite{}
	for _, c := range columns {
		text := c
		if isJSON {
			text = c + "::text"
		}
		// rewrite is this column's whole chain of replacements, given the placeholder it
		// leaves behind: the same text at two widths, because a column at its declared
		// ceiling can hold the narrow one and not the wide one. See bounds.
		rewrite := func(token string) piece {
			out := piece{sql: text}
			for _, m := range ids {
				// Longest-first matching consumes an address before a name inside it.
				// Only owner rows also match bare handles; other rows require @mentions.
				find := "?"
				if m.ownPattern != "" {
					out.args = append(out.args, s.ID, m.ownPattern)
					find = "(CASE WHEN " + own + " THEN ? ELSE ? END)"
				}
				out.sql = "regexp_replace(" + out.sql + ", " + find + ", ?, 'gi')"
				out.args = append(out.args, m.pattern, token)
			}
			if isJSON {
				// Back to jsonb by cast, and an unparseable payload fails here rather than
				// writing a quoted string into a document column.
				out.sql += "::jsonb"
			}
			return out
		}
		wide := rewrite(s.Token())
		set := bound(piece{sql: c + " = "}, wide)
		// A jsonb column carries no bound, so the two cases never meet; the guard says so
		// rather than relying on the catalog never answering for a document column.
		if room := rooms[c]; room.room() && !isJSON {
			// mark is the narrow placeholder on its own, for a value so near its ceiling that the
			// shortened rewrite leaves nothing this package can certify anonymous. See fits.
			set = fits(c, room, wide, rewrite(s.shortToken()), piece{sql: "?", args: []any{s.shortToken()}})
		}
		q.sets = append(q.sets, set.sql)
		q.args = append(q.args, set.args...)
	}

	if g.counter != "" {
		// The row's own counter, added to by the server. No value is bound here, so this
		// half of the SET list cannot move a placeholder out from under an expression.
		q.sets = append(q.sets, g.counter+" = "+g.counter+" + 1")
	}
	if g.stamp != "" {
		// The row's own instant, moved to the clock the caller was built with. Bound here,
		// in the same place in the argument list the rest of the SET list sits at.
		q.sets = append(q.sets, g.stamp+" = ?")
		q.args = append(q.args, g.at)
	}

	filter, filterArgs, err := scoped(s, table, columns, ids, isJSON, where, args)
	if err != nil {
		return nil, err
	}
	q.filter = filter
	// The SET list's values already sit in q.args, and they are the first group of the
	// finished statement: filterArgs carries the caller's own filter values and then this
	// file's terms, in the order their placeholders sit after the SET expressions. Neither
	// this file nor a caller counts a placeholder, which is why that order lives here and
	// nowhere else. Getting it wrong is not a failure anybody sees as one: every value lands
	// one placeholder away from the expression that means it, and the statement either dies
	// on a type the server will not coerce (an address in a uuid column, which is how the
	// auth scrub was found mis-bound) or, worse, runs, matches nothing, and reports a clean
	// erasure.
	q.args = append(q.args, filterArgs...)
	return q, nil
}

// writableNames refuses a name this file would not put into SQL. A contributor passes
// names out of its own source and never out of a request, so this is the reason a table
// or column may be written into a statement at all, and it turns a mistyped claim into a
// refusal at the call instead of a syntax error from the server.
func writableNames(table string, columns []string) error {
	if !namePattern.MatchString(table) {
		return fmt.Errorf("personaldata: %q is not a table name this scrub will write into SQL", table)
	}
	for _, c := range columns {
		if !namePattern.MatchString(c) {
			return fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, c)
		}
	}
	return nil
}

// matchTerms is the test that one row of table names the subject in one of columns: one
// term per column and identifier, joined by scoped below.
//
// Both the rewrite and the re-read ask with these, and now a third statement does too.
// The terms are not a plain substring test but the same pattern the rewrite searches
// with, or a row the rewrite left alone reads as residue and a row it could not reach
// reads as clean. A blank that asked a different question would certify a row clean it
// had never looked at, which is why no caller writes its own WHERE over a table these
// functions already know.
func matchTerms(s Subject, table string, columns []string, ids []match, isJSON bool) ([]string, []any) {
	var terms []string
	var out []any
	own := ownRow(table)
	for _, c := range columns {
		text := c
		if isJSON {
			text = c + "::text"
		}
		for _, m := range ids {
			if m.ownPattern != "" {
				terms = append(terms, "(("+own+" AND "+text+" ~* ?) OR "+text+" ~* ?)")
				out = append(out, s.ID, m.ownPattern)
			} else {
				terms = append(terms, text+" ~* ?")
			}
			out = append(out, m.pattern)
		}
	}
	return terms, out
}

// scoped puts the caller's own filter in front of the match terms and returns the WHERE
// with the values its placeholders stand for: the caller's first, then this package's,
// because the caller's `?` sit in the left half of the finished WHERE.
//
// The arity check is here for the reason it has always had: a filter whose `?` and
// arguments disagree binds every value one placeholder away from the expression that
// means it, and nothing downstream can tell that from a row that simply did not match.
func scoped(s Subject, table string, columns []string, ids []match, isJSON bool,
	where string, args []any) (string, []any, error) {
	terms, termArgs := matchTerms(s, table, columns, ids, isJSON)
	filter := strings.Join(terms, " OR ")
	if strings.TrimSpace(where) != "" {
		if n := strings.Count(where, "?"); n != len(args) {
			return "", nil, fmt.Errorf("personaldata: %s's filter carries %d placeholders and %d arguments", table, n, len(args))
		}
		filter = "(" + where + ") AND (" + filter + ")"
	}
	return filter, append(append(make([]any, 0, len(args)+len(termArgs)), args...), termArgs...), nil
}
