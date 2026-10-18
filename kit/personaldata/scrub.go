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
	"time"
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
//
// One match does not run over the whole tenant however the caller is written: a handle
// read as a bare word is only taken for the person in a row that carries their own id.
// See Subject.Known for why, and ownRow for how.
//
// A column bounded to a declared width is rewritten inside that width, and where even
// the narrower of Subject's two placeholders will not fit, the value is cut to the width
// from its tail: the erasure keeps the row and loses characters that could not have lived
// there anyway, which is the smaller of the two losses open to an operation whose whole
// purpose is to forget. The cut ends at the last character no identifier ends with, so that
// shortening a value cannot spell the person back out of a longer word that stood at its end.
// See bounds and shortest.
func Scrub(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table string,
	columns []string, where string, args ...any) (int64, error) {
	return scrub(ctx, tx, s, guard{}, table, columns, false, where, args)
}

// ScrubTouched is Scrub for a table whose row names the instant it last changed: the same
// rewrite, with the same statement moving the column named by stamp to the clock, because
// a rewrite is a write and a row that says when it last changed has to say when this one
// happened.
//
// files is the table this exists for, and the reason it cannot be left out is the caption
// form. Service.SetAlt refuses a write whose expected instant no longer matches the row's
// own, and an erasure that rewrote alt text in silence would leave a form served before it
// still matching: the next save would write the person's words back over the top of their
// erasure and the row would report that nobody had touched it since. A counter is advanced
// and an instant is moved, which is why the two are two entry points and not one flag —
// there is no way to add one to a timestamp.
func ScrubTouched(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table, stamp string,
	columns []string, where string, args ...any) (int64, error) {
	if !namePattern.MatchString(stamp) {
		return 0, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, stamp)
	}
	return scrub(ctx, tx, s, guard{stamp: stamp, at: db.Now()}, table, columns, false, where, args)
}

// ScrubCounted is Scrub for a table whose row counts its own writes: the same rewrite,
// with the same statement advancing the column named by counter, because a rewrite is a
// write and "this row counts its writes" — the words the migration that gives tasks their
// counter uses for the column — does not except an erasure. entity.RevisionField is the
// kernel's name for such a column.
//
// Moving the counter is what refuses a caller who read the row before the erasure. A form
// served beforehand still holds the revision it was answered with, and a save whose
// expectation still matches would write the erased words back over the top of them and
// leave the row looking as though nobody had touched it since. A table with no counter
// cannot be helped this way — the stale read is then indistinguishable from a current one.
//
// The counter is named by the caller rather than assumed here, for a reason with a body: a
// subject request's row is opened by a command that re-reads its own revision *after* the
// contributors have run (modules/subject/internal/service.go:152-193), so a counter this
// package moved under that command would leave its guarded write matching no row at all,
// and the receipt of the erasure would be lost without an error anywhere. Naming the column
// is the reading that precedes opting a table in.
func ScrubCounted(ctx context.Context, tx db.Tx[db.Tenant], s Subject, table, counter string,
	columns []string, where string, args ...any) (int64, error) {
	if !namePattern.MatchString(counter) {
		return 0, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, counter)
	}
	return scrub(ctx, tx, s, guard{counter: counter}, table, columns, false, where, args)
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
	return scrub(ctx, tx, s, guard{}, table, columns, true, where, args)
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
	q, err := query(s, guard{}, table, columns, isJSON, nil, where, args)
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

// match is one replacement the scrub makes in one column: what to look for, and — for the
// one string that cannot be told from an ordinary word — which rows may be searched for it.
type match struct {
	// pattern is what every row of the scope is searched for.
	pattern string

	// ownPattern is what only a row carrying the person's own id is searched for, and it is
	// set only beside a handle: read as a bare word it is the person in their own rows and
	// somebody's verb in everybody else's. The row that does not carry the id keeps the
	// narrower pattern above, so the two live in one expression the server decides per row.
	ownPattern string
}

// matches expands what a person is known by into the replacements to make, longest
// identifier first: an address and a name as whole words wherever they sit, and a handle as
// the @-mention it always is, or — in a row that names the person by id — as the bare word
// too. See Subject.Known.
//
// For a jsonb column each identifier joins a second time, as the document's serialisation
// spells it: see jsonEscaped.
func matches(s Subject, isJSON bool) []match {
	ids := s.Known()
	if isJSON {
		// Appended rather than replacing, so both spellings are searched in one statement:
		// a document that arrived through a client that had already escaped once holds the
		// raw text inside the serialised one, and a scrub of a jsonb column that reached only
		// for the stored text would certify that row clean.
		escaped := make([]Identifier, 0, len(ids))
		for _, id := range ids {
			if e := jsonEscaped(id.Value); e != id.Value {
				escaped = append(escaped, Identifier{Value: e, Login: id.Login})
			}
		}
		ids = append(ids, escaped...)
	}
	out := make([]match, 0, len(ids))
	for _, id := range ids {
		if id.Login {
			out = append(out, match{pattern: mentionPattern(id.Value), ownPattern: boundedPattern(id.Value)})
			continue
		}
		out = append(out, match{pattern: boundedPattern(id.Value)})
	}
	return out
}

// jsonEscaped is how Postgres writes one string when it serialises a jsonb document, which
// is the text a scrub of a jsonb column searches: `payload::text` is not the string that
// went in, and a name containing a quote or a backslash is not the same bytes on both sides
// of that cast. `Jo "JJ" Jones` is stored as `{"displayName":"Jo \\"JJ\\" Jones"}` — in the
// text the scrub runs over, `Jo \"JJ\" Jones` — so the raw pattern matches nothing there and
// the person's whole name survives an erasure that reported success.
//
// It is written out rather than produced by encoding/json because the two serialisers do not
// agree, and the one that decides what is in the column is the server: Go escapes U+2028 and
// U+2029 and, by default, `<`, `>` and `&`, none of which jsonb escapes. A name with a `&`
// in it is not rare, and a pattern for a spelling the document never holds leaves the name
// where it was while the re-read asks the same pattern and answers that nothing is left.
func jsonEscaped(s string) string {
	var out strings.Builder
	out.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&out, `\u%04x`, r)
				continue
			}
			out.WriteRune(r)
		}
	}
	return out.String()
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
	if !namePattern.MatchString(table) {
		return nil, fmt.Errorf("personaldata: %q is not a table name this scrub will write into SQL", table)
	}
	for _, c := range columns {
		if !namePattern.MatchString(c) {
			return nil, fmt.Errorf("personaldata: %s.%s is not a column name this scrub will write into SQL", table, c)
		}
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
	var terms []string
	var termArgs []any
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
				// Identifiers are longest first, so a name inside the person's own address
				// goes with the address and not after it, which would leave the tail of a
				// mailbox in a note about somebody. boundedPattern is what lets an address
				// full of dots, pluses and question marks be a pattern at all, and stops that
				// pattern finding a colleague's word that merely contains them.
				//
				// A handle is the one string whose pattern the row itself picks: the CASE below
				// asks the server, per row, whether this row carries the person's id, and a row
				// that does is searched for the bare word as well as the mention. One expression
				// decides both halves, which is what makes the re-read below mean what it says.
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
		for _, m := range ids {
			// The same pattern the rewrite used, and not a plain substring test: the
			// re-read has to ask what the rewrite would find, or a row the rewrite left
			// alone reads as residue and a row it could not reach reads as clean.
			if m.ownPattern != "" {
				terms = append(terms, "(("+own+" AND "+text+" ~* ?) OR "+text+" ~* ?)")
				termArgs = append(termArgs, s.ID, m.ownPattern)
			} else {
				terms = append(terms, text+" ~* ?")
			}
			termArgs = append(termArgs, m.pattern)
		}
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

// fits rewrites one bounded column inside the room the server says it holds: the wide
// placeholder where it fits, and the narrow one shortened to the room where even that will
// not go. See bounds for where the room comes from, and Scrub for why the erasure cuts
// rather than failing.
//
// The rewrites are written once each, into the FROM of a sub-select, and the CASE below reads
// their names. Writing them out again at the place they are measured would ask the server for
// the same chain of regexp_replace twice over a column a quarter of a megabyte wide. The room
// is then applied to what came back: the narrow rewrite is cut to the character bound first,
// so that where the column also bounds bytes the measurement that decides the second cut is
// taken on the text that would actually be written.
//
// mark is the narrow placeholder on its own, which is what a column takes when shortening the
// rewrite leaves nothing behind that this file can vouch for. See shortest.
func fits(column string, room extent, wide, narrow, mark piece) piece {
	var when []piece
	if room.chars > 0 {
		when = append(when, piece{sql: "length(room.wide) <= ?", args: []any{room.chars}})
	}
	if room.octets > 0 {
		when = append(when, piece{sql: "octet_length(room.wide) <= ?", args: []any{room.octets}})
	}
	guards := []piece{{sql: column + " = (SELECT CASE WHEN "}}
	for _, clause := range when {
		if len(guards) > 1 {
			guards = append(guards, piece{sql: " AND "})
		}
		guards = append(guards, clause)
	}
	cut, token := narrow, mark
	if room.chars > 0 {
		cut = bound(piece{sql: "left("}, narrow, piece{sql: ", ?)", args: []any{room.chars}})
		token = bound(piece{sql: "left("}, mark, piece{sql: ", ?)", args: []any{room.chars}})
	}
	sets := append(guards, piece{sql: " THEN room.wide ELSE "}, shortest(room),
		piece{sql: " END FROM (SELECT "}, wide,
		piece{sql: " AS wide, "}, cut,
		piece{sql: " AS cut, "}, token,
		piece{sql: " AS mark) AS room)"},
	)
	return bound(sets...)
}

// shortest is the value to write when the wide placeholder will not fit: the cut the room
// already took, taken down to the byte bound too where the column carries one, and then taken
// back to the last character that could not have stood inside one of the person's identifiers.
// That last step is what keeps a shortened value anonymous; see outsideWord.
//
// Where even that leaves nothing — a value that is one unbroken word the whole way to its
// ceiling — the column takes the placeholder rather than an empty string, so the row still says
// an erased person sat in it and a product that holds its column to a minimum length finds its
// erasure refused by nothing.
func shortest(room extent) piece {
	return bound(
		piece{sql: "coalesce(nullif("}, outsideWord(byteRoom("room.cut", room)),
		piece{sql: ", ''), "}, byteRoom("room.mark", room), piece{sql: ")"},
	)
}

// outsideWord is the value with its trailing run of identifier characters taken back.
//
// A cut is not a rewrite, and the difference is the one that matters: a rewrite leaves no
// identifier standing, and a cut can put one back. "Jo Jones: … / Jo Joneson" names two people,
// and erasing the first leaves the second alone, correctly, because Jo Joneson is not the
// person being forgotten. Cut to the title's ceiling the value ends "… / Jo Jones", and the end
// of a value is a word boundary, so the characters the cut removed were the ones that had been
// telling the server this was a longer name. The cut had rebuilt the name it existed to remove,
// and the erasure reported itself finished over the top of it.
//
// So the cut gives back the tail it cannot reason about and keeps everything up to the last
// character no identifier ends with. Nothing the row was certain of is lost: every character
// given back stood against the cut, so it is the shortest tail that could have been standing
// inside one of the person's own strings.
func outsideWord(value piece) piece {
	return bound(piece{sql: "regexp_replace("}, value,
		piece{sql: ", ?, '', 'g')", args: []any{identifierRun}})
}

// identifierRun is the tail outsideWord takes back: the characters an identifier is built from,
// which are the characters a value can end with while still naming the person.
//
// `[[:alnum:]_]` is the whole of it for a name and for the mention of a handle, and it is the
// server's own word rather than a translation of one — the same class `\m` and `\M` decide a
// boundary against, on the same database this statement runs. A name cut off at a letter, a
// digit or an underscore is a word again at the end of a value, and that is what the boundary
// rule would then find.
//
// The dot is no word character and is in the class for one pattern alone: a handle is matched
// as `@handle`, with the match refused when a dot and a letter follow, because "@will.co" is
// somebody else's mailhost. A cut one character past that dot ends the mention and removes the
// reason the refusal applied, so the run reaches over the dot. A name whose own last character
// is punctuation needs none of this: it is matched as a substring on that side, which the
// characters before the cut already satisfied and the rewrite already took.
const identifierRun = `[[:alnum:]_.]+$`

// byteRoom takes the characters a byte bound finds over the bound, counting them off the tail of
// the named column of fits's sub-select. It measures characters rather than bytes because these
// are text columns and a character is what a reader loses: dropping a character drops at least
// one byte, so dropping as many characters as there are bytes over the bound is the smallest cut
// that is always enough, and the only one that never touches a character the row was allowed to
// keep. Where the column bounds characters alone, the named value is already inside the room.
func byteRoom(name string, room extent) piece {
	value := piece{sql: name}
	if room.octets == 0 {
		return value
	}
	return bound(piece{sql: "left("}, value,
		piece{sql: ", greatest(length("}, value,
		piece{sql: ") - greatest(octet_length("}, value,
		piece{sql: ") - ?, 0), 0))", args: []any{room.octets}})
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

// mentionPattern quotes a handle that is written as one — with the @ in front of it — and
// nothing else.
//
// The @ is the whole difference: it is the form a handle takes in prose that cannot also be
// the word itself, so it is the one handle match that runs over the whole tenant. The
// closing boundary keeps "@will" from finding "@william", and the refusal of a dot followed
// by a letter keeps it from finding the domain of somebody's address: "@will.co" is a
// mailhost somebody else receives mail on, and an erasure that rewrote it would be destroying
// a third party's data to erase one person.
func mentionPattern(id string) string {
	out := "@" + escapePattern(id)
	if last, _ := utf8.DecodeLastRuneInString(id); isWordRune(last) {
		out += `\M(?!\.[[:alnum:]])`
	}
	return out
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
