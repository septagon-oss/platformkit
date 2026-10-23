package db

import (
	"slices"
	"strings"
)

// cteLead is the offset just past the `WITH` — and the `RECURSIVE` that may follow it — that
// opens a body's statement, whether the body said RECURSIVE, and whether it opened with a CTE
// list at all. Whitespace and commentary are walked with sqlToken, the reader every other
// boundary in this package takes, so a body that opens with a comment is not read as opening
// with the words of the comment. The only statement PostgreSQL opens with the word `with` is
// one carrying a common table expression list, so anything else at the front is an ordinary
// body and the answer says so rather than guessing.
func cteLead(body string) (after int, recursive bool, opens bool) {
	word, next, found := sqlWord(body, 0)
	if !found || word != "with" {
		return 0, false, false
	}
	if word, later, found := sqlWord(body, next); found && word == "recursive" {
		return later, true, true
	}
	return next, false, true
}

// sqlWord reads the next word of text at or after i — commentary and whitespace walked over,
// the word lower-cased the way every other reading of a file's SQL is — and the offset past
// it. A character that starts no word (a parenthesis, a quote the scanner did not take) is an
// answer of false: this is not a word, and the caller reads nothing from it.
func sqlWord(text string, i int) (word string, after int, found bool) {
	for i < len(text) {
		if width := sqlToken(text, i); width > 0 {
			i += width
			continue
		}
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' {
			i++
			continue
		}
		start := i
		for i < len(text) && (text[i] == '_' || text[i] == '$' ||
			(text[i] >= 'a' && text[i] <= 'z') || (text[i] >= 'A' && text[i] <= 'Z') ||
			(text[i] >= '0' && text[i] <= '9')) {
			i++
		}
		return strings.ToLower(text[start:i]), i, i > start
	}
	return "", i, false
}

// windowRelation is the name the drain's own window carries inside the statement it
// assembles (windowedBody), and therefore the name a body's CTE list may take once: the
// merged list carries both. `reBatchWindow` reads the name wherever the SQL selects from
// it; this is the name the drain writes.
const windowRelation = "batch"

// bindsWindowRelation checks the body's top-level CTE names for the reserved
// window relation. It accepts quoted names and column lists, respects quoted case,
// and ignores nested scopes, which do not collide with the wrapper's CTE.
func bindsWindowRelation(body string) bool {
	at, _, opens := cteLead(body)
	if !opens {
		return false
	}
	for {
		name, after, found := sqlNameAt(body, at)
		if !found {
			return false
		}
		if open := sqlSpace(body, after); open < len(body) && body[open] == '(' {
			at = pastParens(body, open)
		} else {
			at = open
		}
		word, after, found := sqlWord(body, at)
		if !found || word != "as" {
			return false
		}
		open := sqlSpace(body, after)
		if open >= len(body) || body[open] != '(' {
			return false
		}
		if name == windowRelation {
			return true
		}
		at = sqlSpace(body, pastParens(body, open))
		if at >= len(body) || body[at] != ',' {
			return false
		}
		at++
	}
}

// movesTheWindowKey detects assignments to the cursor key and inserts or merges
// into the drained table; either can put completed work ahead of the cursor.
// Reads and deletes are allowed. Indirect writes through views, functions or
// triggers are outside this lexical check and remain bounded by the drain budget.
// The data-body-unbounded allowance opts out of windowing and this check.
func movesTheWindowKey(body, table, column string) windowKeyMove {
	for at := 0; at < len(body); {
		word, after, found := sqlWord(body, at)
		if !found {
			at++
			continue
		}
		at = after
		switch word {
		case "update":
			if set, ok := setList(body, after, table); ok && setWritesColumn(body, set, column) {
				return keyWritten
			}
		case "insert", "merge":
			if statementAddsRowsTo(body, after, table) {
				return rowsAdded
			}
		}
	}
	return keyHolds
}

// windowKeyMove is that answer, with the two sentences the refusal prints: what the body did to
// the key set, and what its author does instead. One refusal, because the harm is one harm — the
// set the cursor reasons over has moved — and two remedies, because the file shape that caused it
// is not one shape.
type windowKeyMove uint8

const (
	keyHolds windowKeyMove = iota
	keyWritten
	rowsAdded
)

func (m windowKeyMove) found() bool { return m != keyHolds }

// wrote names the act the drain cannot bound.
func (m windowKeyMove) wrote() string {
	switch m {
	case keyWritten:
		return "this body writes that column, so the rows it touches land above the cursor and every window behind this one rewrites them"
	case rowsAdded:
		return "this body puts new rows into that table, so keys no window ordered yet stand above the cursor and every window behind this one drains rows the last one created"
	}
	return ""
}

// instead names the file the author can ship instead.
func (m windowKeyMove) instead() string {
	switch m {
	case keyWritten:
		return "a backfill writes the columns a key holds, not the key it holds — re-keying a table is a schema file or a job of its own, one that keeps its own position"
	case rowsAdded:
		return "a backfill writes the rows a window holds, not new ones — a file that adds rows to the table it drains has no window that can bound it, so except data-body-unbounded and bound the statement itself, or make the append a job of its own"
	}
	return ""
}

// statementAddsRowsTo says whether the statement whose `insert` or `merge` ended at `at` puts rows
// into the drained table. The reader walks the `INTO` the verb may carry rather than asking which
// verb requires it: `MERGE INTO` does, and PostgreSQL refuses an `INSERT` written without it
// (measured at the pinned version), so one walk covers both spellings and depends on neither. The
// schema qualification is walked and dropped the way an UPDATE's is — this question is about
// which table, not whose schema — and a name the reader cannot read names nothing, which is an
// answer of no rather than a guess.
func statementAddsRowsTo(text string, at int, table string) bool {
	if word, after, ok := sqlWord(text, at); ok && word == "into" {
		at = after
	}
	name, _, found := targetName(text, at)
	return found && name == table
}

// targetName is the name a statement aims at, walked through the spellings that name one table
// without changing which: `ONLY`, the parenthesis `ONLY` may take, a schema qualification
// (whose last name is the table's) and the `*` PostgreSQL allows after a table's name.
func targetName(text string, at int) (string, int, bool) {
	name, next, found := sqlNameAt(text, at)
	if !found {
		return "", at, false
	}
	if name == "only" {
		if open := sqlSpace(text, next); open < len(text) && text[open] == '(' {
			next = open + 1
		}
		if name, next, found = sqlNameAt(text, next); !found {
			return "", next, false
		}
	}
	for dot := sqlSpace(text, next); dot < len(text) && text[dot] == '.'; dot = sqlSpace(text, next) {
		if name, next, found = sqlNameAt(text, dot+1); !found {
			return "", next, false
		}
	}
	if star := sqlSpace(text, next); star < len(text) && text[star] == '*' {
		next = star + 1
	}
	return name, next, true
}

// setList is the offset just past the `set` that opens an UPDATE's assignment list, given the
// text after the word `update` and the table the window drains. The target is walked with the
// same reader an INSERT's is, and the alias the target carries is walked with or without the word
// `AS`, because PostgreSQL takes both: `UPDATE probe p SET …` names this table as plainly as
// `UPDATE probe AS p SET …` does, and a reader that asked for the word missed the re-key written
// without it. What PostgreSQL does *not* take, measured at the pinned version rather than
// assumed, is a column alias list after that alias — `UPDATE probe AS p (id, note) SET …` is a
// syntax error there — so no reading of one belongs here. A statement whose target the reader
// cannot name — an upsert's `DO UPDATE`, a `MERGE` arm — is not an UPDATE of this table as far as
// *this* reading is concerned; both are answered at the statement that carries them
// (statementAddsRowsTo), which names the table they write.
func setList(text string, at int, table string) (int, bool) {
	name, next, found := targetName(text, at)
	if !found || name != table {
		return 0, false
	}
	if close := sqlSpace(text, next); close < len(text) && text[close] == ')' {
		next = close + 1
	}
	if word, after, ok := sqlWord(text, next); ok && word == "as" {
		if _, alias, ok := sqlNameAt(text, after); ok {
			next = alias
		}
	} else if ok && word != "set" {
		if _, alias, ok := sqlNameAt(text, next); ok {
			next = alias
		}
	}
	word, past, ok := sqlWord(text, next)
	if !ok || word != "set" {
		return 0, false
	}
	return past, true
}

// setWritesColumn reads the assignment list a `set` opens at from and says whether one of its
// targets is column. The list ends where a comma the list itself holds stops opening items or
// where a word ends the statement; the walk counts parentheses, because a call inside a value
// keeps its own commas, and walks over everything sqlToken reads as one construct, because a
// parenthesis, a comma or a keyword inside a value is data the body is writing rather than
// structure this reader may read.
func setWritesColumn(text string, from int, column string) bool {
	for at := from; ; {
		var targets []string
		open := sqlSpace(text, at)
		if open >= len(text) {
			return false
		}
		if text[open] == '(' {
			end := pastParens(text, open)
			targets = namesIn(text[open+1 : end])
			at = end
		} else {
			name, next, found := sqlNameAt(text, open)
			if !found {
				return false
			}
			targets, at = []string{name}, next
		}
		eq := sqlSpace(text, at)
		if eq >= len(text) || text[eq] != '=' {
			return false
		}
		if slices.Contains(targets, column) {
			return true
		}
		at = pastAssignment(text, eq+1)
		if at >= len(text) || text[at] != ',' {
			return false
		}
		at++
	}
}

// pastAssignment is where one assignment's value ends: the comma that opens the next item, the
// word that ends the statement, the semicolon that ends the body — whichever comes first at the
// depth the list is written at. A value's own parentheses close before its depth, so
// `SET x = coalesce(a, b), id = 1` ends its first item at the comma the list holds and not at
// the one the call holds.
func pastAssignment(text string, from int) int {
	depth := 0
	for i := from; i < len(text); {
		if width := sqlToken(text, i); width > 0 {
			i += width
			continue
		}
		switch text[i] {
		case '(':
			depth++
			i++
			continue
		case ')', ',', ';':
			if depth == 0 {
				return i
			}
			depth--
		}
		if depth == 0 {
			if word, _, found := sqlWord(text, i); found && endsAssignment[word] {
				return i
			}
		}
		i++
	}
	return len(text)
}

// endsAssignment are the words that stand between an UPDATE's last assignment and the rest of
// its statement. `FROM` is PostgreSQL's own UPDATE … FROM, and the tail is the select the
// statement may be wrapped in; a word outside this list is part of a value, and the walk reads
// on rather than end the list early on a column named after one of them.
var endsAssignment = map[string]bool{
	"where": true, "returning": true, "from": true, "group": true, "order": true,
	"limit": true, "offset": true, "fetch": true, "window": true, "for": true,
	"union": true, "intersect": true, "except": true, "having": true,
}

// namesIn is every name a comma-separated list holds — the column list of a multi-column
// assignment, whose targets are the whole of it. Each item's first name is its column: a
// parenthesised row constructor inside one is a value, and this reader is asked about targets.
func namesIn(list string) []string {
	var out []string
	for _, item := range splitClauses(list) {
		if name, _, found := sqlNameAt(item, 0); found {
			out = append(out, name)
		}
	}
	return out
}

// sqlSpace is the offset of the next byte of text at or after i that stands in the statement's
// own grammar — the position a reader looks at to see what comes next. Whitespace, commentary,
// dollar-quoted values and ordinary string literals are gone because none of them is structure:
// a body that puts a comment between a CTE's name and its `AS`, or stores the words `set id =`
// in a column, is a body whose structure this reader still reads and whose data it never reads.
// A quoted name is *not* walked over, because it is a name: the spelling PostgreSQL takes for a
// column called `id` when something else already owns the letters, and the one spelling a reader
// that skipped its quotes would lose.
func sqlSpace(text string, i int) int {
	for i < len(text) {
		switch {
		case text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r':
			i++
		case strings.HasPrefix(text[i:], "--"), strings.HasPrefix(text[i:], "/*"),
			dollarTag(text[i:]) != "", text[i] == '\'':
			i += sqlToken(text, i)
		default:
			return i
		}
	}
	return i
}

// sqlNameAt reads the next name in text at or after i in either spelling PostgreSQL takes it,
// resolved the way the server resolves it, and the offset just past it: an unquoted name
// folded to lower case, which is what the database does to one, and a quoted name with the
// letters its author wrote and the doubled quotes taken back, which is what it does not. Both
// halves matter here and in opposite directions: folding `"Batch"` would refuse a body whose
// own relation the window does not collide with, and refusing to fold `BATCH` would miss the
// body that does.
func sqlNameAt(text string, i int) (name string, after int, found bool) {
	for i < len(text) {
		if text[i] == '"' {
			width := sqlToken(text, i)
			if width == 0 || !strings.HasSuffix(text[i:i+width], `"`) {
				return "", i, false
			}
			quoted, _ := sqlIdent(text[i : i+width])
			return quoted, i + width, true
		}
		if width := sqlToken(text, i); width > 0 {
			i += width
			continue
		}
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' {
			i++
			continue
		}
		if !isIdentStart(text[i]) {
			return "", i, false
		}
		start := i
		for i < len(text) && isIdentByte(text[i]) {
			i++
		}
		return strings.ToLower(text[start:i]), i, true
	}
	return "", i, false
}

// pastParens is the offset one step past the parenthesised group opening at i — nesting
// counted, and every construct sqlToken reads as one token walked over whole, because a
// parenthesis inside a value or a comment is data and not the group's end. An unclosed group
// runs to the end of the text, which is what the server is left with there too, and it refuses
// the file rather than run it.
func pastParens(text string, i int) int {
	depth := 0
	for i < len(text) {
		if width := sqlToken(text, i); width > 0 {
			i += width
			continue
		}
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
		i++
	}
	return len(text)
}
