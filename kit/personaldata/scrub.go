package personaldata

// This file holds the two statements every contributor's Erase is made of, so the
// eight owners of a person's rows do not each write their own answer to "remove
// what this row says about this person". One implementation is the house rule, and
// here it is also the only way the rule can be checked: the erasure's verification
// (ADR 0020 §4) has to ask the same question the scrub asked, and eight copies can
// only drift.
//
// Both statements run in the caller's transaction, which is the transaction row-level
// security already bounded to one tenant, so neither names tenant_id: the policy is
// the bound, and a hand-written filter would be a second, weaker answer to a question
// the database already answers.

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/septagon-oss/platformkit/kit/db"
)

// namePattern is what a table or column name is allowed to look like. A contributor
// passes names out of its own source and never out of a request, so this is not a
// defence against injection: it is the reason these functions may write a name into
// SQL at all, and it turns a mistyped claim into a refusal at the call instead of a
// syntax error from the server.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Scrub replaces every identifier the subject is known by with Subject.Token() in the
// named text columns of one table, for the rows where names, and returns how many rows
// it changed.
//
// The match is a case-insensitive substring match: an address somebody typed into a
// note as "Jo.Jones@Example.COM" is the same mailbox, and a rule that found only the
// spelling the users row happened to carry would leave the others behind while
// reporting success. What it is not is inference — a note that identifies a person by
// describing them is not findable by any mechanical rule, and ADR 0020 says so rather
// than overpromising.
//
// where is a SQL fragment over the table's own columns, with `?` placeholders filled
// from args, or empty. Empty is not an oversight: a row that names a person need not
// carry their id — the person a task's description is about is not its assignee — so a
// text column is scrubbed over everything the tenant can see. It is also what lets
// Residue ask the same question back and mean it.
func Scrub(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return scrub(ctx, tx, s, table, columns, false, where, args)
}

// ScrubJSON is Scrub for a jsonb column, which is one cast away and no further: the
// scrub is a text operation on the document's serialisation and the value has to be
// told it is a jsonb column again.
//
// It is a second entry point and not a flag because the cast is the whole difference
// and a caller must not be able to forget it. What the replacement can reach is any
// string the document holds — an address sits in audit_events.payload verbatim today —
// and the token is angle-bracketed text, which stays inside the JSON string it
// replaces, so the document that comes back is the one that went in with a word
// changed. A payload whose stored text does not parse as jsonb is refused by the cast,
// which is the right answer: an erasure that could not read a row cannot certify it.
func ScrubJSON(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return scrub(ctx, tx, s, table, columns, true, where, args)
}

// Residue counts the rows of one table that still carry one of the subject's
// identifiers in the named columns.
//
// It is the erasure's re-read (ADR 0020 §4): a command that scrubbed the columns of its
// own declaration asks this afterwards, inside the same transaction, and a non-zero
// answer means somebody declared a column and did not clean it. Rolling back on that
// answer is what makes a Text claim a promise instead of a comment.
func Residue(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return rows(ctx, tx, s, table, columns, false, where, args)
}

// ResidueJSON is Residue for a jsonb column.
func ResidueJSON(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return rows(ctx, tx, s, table, columns, true, where, args)
}

func scrub(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, isJSON bool, where string, args []any) (int64, error) {
	q, err := query(s, table, columns, isJSON, where, args)
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
	q, err := query(s, table, columns, isJSON, where, args)
	if err != nil {
		return 0, err
	}
	if q == nil {
		return 0, nil
	}
	sql := "SELECT count(*) FROM " + table
	if q.filter != "" {
		sql += " WHERE " + q.filter
	}
	var n int64
	if err := tx.DB().WithContext(ctx).Raw(sql, q.args...).Scan(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
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

// query writes the rewrite, and refuses one whose caller mis-counted its own filter.
//
// A nil statement with no error is the honest no-op: a person with no email, no name
// and no handle has no string to hunt, and a table with no text columns to look in has
// nowhere to hunt — rewriting everything, or nothing, would both be a lie.
func query(s Subject, table string, columns []string, isJSON bool,
	where string, args []any) (*rewrite, error) {
	if !namePattern.MatchString(table) {
		return nil, fmt.Errorf("personaldata: %q is not a table name this scrub will write into SQL", table)
	}
	for _, c := range columns {
		if !namePattern.MatchString(c) {
			return nil, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, c)
		}
	}
	ids := s.Identifiers()
	if len(ids) == 0 || len(columns) == 0 {
		return nil, nil
	}

	// Every value is bound at the place it is written, in the order the finished SQL
	// asks for it, and a value used twice is bound twice. Neither this file nor a
	// caller ever counts a placeholder, which is why the caller's own arguments go
	// last: their `?` sit in the filter, written after the SET expressions.
	q := &rewrite{}
	var terms []string
	var termArgs []any
	for _, c := range columns {
		text := c
		if isJSON {
			text = c + "::text"
		}
		expr := text
		for _, id := range ids {
			// Identifiers are longest first, so a name inside the person's own address
			// goes with the address and not after it, which would leave the tail of a
			// mailbox in a note about somebody. boundedPattern is what lets an address
			// full of dots, pluses and question marks be a pattern at all, and stops that
			// pattern finding a colleague's word that merely contains them.
			expr = "regexp_replace(" + expr + ", ?, ?, 'gi')"
			q.args = append(q.args, boundedPattern(id), s.Token())
		}
		if isJSON {
			// Back to jsonb by cast, and an unparseable payload fails here rather than
			// writing a quoted string into a document column.
			expr += "::jsonb"
		}
		q.sets = append(q.sets, c+" = "+expr)
		for _, id := range ids {
			// The same pattern the rewrite used, and not a plain substring test: the
			// re-read has to ask what the rewrite would find, or a row the rewrite left
			// alone reads as residue and a row it could not reach reads as clean.
			terms = append(terms, text+" ~* ?")
			termArgs = append(termArgs, boundedPattern(id))
		}
	}

	q.filter = strings.Join(terms, " OR ")
	if strings.TrimSpace(where) != "" {
		if n := strings.Count(where, "?"); n != len(args) {
			return nil, fmt.Errorf("personaldata: %s's filter carries %d placeholders and %d arguments", table, n, len(args))
		}
		q.filter = "(" + where + ") AND (" + q.filter + ")"
	}
	// The three groups are appended in the order their placeholders sit in the finished
	// SQL, which is one SET list, then the caller's own filter, then this file's own
	// terms. The caller's `?` are in the left half of the WHERE, so their values go
	// before the terms', not after them. Neither this file nor a caller counts a
	// placeholder, which is why that order lives here and nowhere else. Getting it wrong
	// is not a failure anybody sees as one: every value lands one placeholder away from
	// the expression that means it, and the statement either dies on a type the server
	// will not coerce (an address in a uuid column, which is how the auth scrub was found
	// mis-bound) or, worse, runs, matches nothing, and reports a clean erasure.
	q.args = append(q.args, args...)
	q.args = append(q.args, termArgs...)
	return q, nil
}

// boundedPattern quotes an identifier for Postgres' ARE pattern syntax and puts a word
// boundary around it.
//
// Two jobs, and the second is the one that keeps other people's writing. Unbounded,
// erasing a person called Ann whose handle is "ann" rewrites a colleague's "Annual
// planning" as "<erased …>ual pl<erased …>ing" in every text column of the tenant: data
// that belongs to somebody else, destroyed by an operation that cannot be undone. What
// the ADR's substring rule is for is finding "Jo.Jones@Example.COM" as the same mailbox
// as the lower-case spelling, and a case-insensitive whole-word match still finds both.
// A word inside the person's own name is still found: "Mary Ann Smith" is erased by the
// identifier "Mary Ann Smith" as one whole, and the longest-first order above is what
// puts the address before the name that sits inside it.
//
// \m and \M are zero-width, so two mentions side by side are both found, where a pattern
// that swallowed the space between them would find only the first. They are also only
// honest beside a word character, and "word" is the server's locale-dependent answer.
// So a boundary is written only where this file can say what the server will make of the
// side it sits against, read as a whole rune and not as a byte: an identifier that begins
// or ends with a letter, digit or underscore in any script is bounded, and one that begins
// or ends with punctuation is bounded on the side it can be and matched as a substring on
// the side it is not. Over-broad leaves a token in a word nobody meant; under-broad leaves
// the person's own address in somebody's notes. Only one of those is the failure this
// contract exists to prevent.
//
// Reading the byte instead of the rune was the bug this comment is written against: the
// last byte of "René" is the second byte of é, which looked "not a word character" to an
// ASCII test, so no closing boundary was written and erasing René rewrote the first four
// letters of a colleague called Renée — the substring corruption the whole boundary rule
// exists to stop, arriving through a name with an accent on it. The reading is now the
// server's own: this tree's database is created ENCODING 'UTF8' with a UTF-8 ctype, where
// `\m`/`\M` count é as a word character (`'Renée' ~* '\mRené\M'` answers false there, and
// `'René' ~* 'é\M'` answers true), so a letter of any script is bounded here and honoured
// there. A deployment on a database whose ctype calls accented letters non-word would lose
// the closing boundary and leave a mention unerased rather than corrupt a neighbour — the
// direction this rule keeps failing towards — and the residue re-read asks the same
// pattern, so such a mention is at least reported rather than certified clean.
//
// escapePattern is the other job, and is described beside it.
func boundedPattern(id string) string {
	first, _ := utf8.DecodeRuneInString(id)
	last, _ := utf8.DecodeLastRuneInString(id)
	var out strings.Builder
	out.Grow(len(id) + 16)
	if isWordRune(first) {
		out.WriteString(`\m`)
	}
	out.WriteString(escapePattern(id))
	if isWordRune(last) {
		out.WriteString(`\M`)
	}
	return out.String()
}

// isWordRune reports whether Postgres' ARE word boundary would count r as a word
// character on a database in a UTF-8 locale: `[:alnum:]` and the underscore, which is the
// same set this file can decide without a server, and which is why the check is a rune one
// — see boundedPattern.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// escapePattern quotes an identifier for Postgres' ARE pattern syntax: every
// metacharacter it could mean gets a backslash, so a person's address — a legal place
// for a dot, a plus, a bracket and a question mark — matches itself and nothing else.
//
// It happens here and not in SQL because Postgres carries no regexp_quote to do it
// server-side (the first run of this file found that out with a refusal), and doing it
// in Go is the version that can be read and tested without a database. The token a
// scrub leaves behind carries no backslash and no digit, so the replacement side of the
// expression needs no such treatment.
func escapePattern(id string) string {
	var out strings.Builder
	out.Grow(len(id) + 8)
	for i := 0; i < len(id); i++ {
		switch id[i] {
		case '.', '\\', '+', '*', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$':
			out.WriteByte('\\')
		}
		out.WriteByte(id[i])
	}
	return out.String()
}
