package db

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The rule table is the review a migration cannot do for itself: the statements that
// stop a running application while they run, and the shapes that make an
// expand/contract release unsafe. A rule names itself, says what the file does, says
// what to do instead, and — where the judgement is the owner's — how to except it.
//
// It reads text with `--` comments stripped, not a parse tree, because the runner is
// not a SQL parser (docs/adr/0011). A statement inside a dollar-quoted body can
// therefore produce a false positive, and the answer to that is the marker — which is
// also why every correctable rule has one.
type rule struct {
	name    string
	does    string
	instead string
	// exception names the file shape that fixes the rule where there is one. It is
	// empty for the rules whose remedy is not a marker: one Postgres itself enforces,
	// or one whose exception would be a claim about a risk that does not exist.
	exception string
	// correctable says whether the file may keep the statement at all.
	fire func(f *migrationText) bool
}

// migrationText is one file as the rule table reads it: comments gone, statements
// split, lower-cased, the tables this very file creates known by name, and the window
// question already answered for the whole file rather than once per rule.
type migrationText struct {
	phase      string
	autocommit bool
	body       string
	windowed   bool
	statements []string
	created    map[string]bool
}

var (
	// reAlterTable and reColumnTypeClause are the two halves of the statement the
	// first rule is about. PostgreSQL makes the COLUMN keyword optional and spells
	// the clause either `TYPE` or `SET DATA TYPE`; every spelling of it rewrites the
	// whole table under ACCESS EXCLUSIVE, so the predicate reads the clause inside an
	// ALTER TABLE statement rather than one spelling of the keyword.
	reAlterTable       = regexp.MustCompile(`^alter\s+table\b`)
	reColumnTypeClause = regexp.MustCompile(`\balter\s+(column\s+)?[a-z_][\w.$]*\s+(set\s+data\s+)?type\b`)
	// reAddColumnAction is `ADD [COLUMN] <name>` in either spelling PostgreSQL
	// takes. The name is captured because the word after ADD says what is being
	// added: `ADD CONSTRAINT x CHECK (… IS NOT NULL)` adds a table constraint, not a
	// NOT NULL column, and reads as neither.
	reAddColumnAction = regexp.MustCompile(`\badd\s+(?:column\s+)?([a-z_][\w.]*)(?:\s|$)`)
	reNotNull         = regexp.MustCompile(`\bnot\s+null\b`)
	reDefault         = regexp.MustCompile(`\bdefault\b`)
	reCreateIndex     = regexp.MustCompile(`^create\s+(unique\s+)?index\b`)
	reDropIndex       = regexp.MustCompile(`^drop\s+index\b`)
	reConcurrently    = regexp.MustCompile(`\bconcurrently\b`)
	reIndexTarget     = regexp.MustCompile(`\bon\s+([a-z_][\w.]*)`)
	reCreateTable     = regexp.MustCompile(`create\s+table\s+(if\s+not\s+exists\s+)?([a-z_][\w.]*)`)
	// reDropAction is `DROP [COLUMN] <name>` in either spelling PostgreSQL takes, the
	// keyword being as optional here as it is in the two ALTER TABLE actions above. The
	// name is captured because the word after DROP says what is being dropped, and only
	// a column takes a name away from the running release.
	reDropAction  = regexp.MustCompile(`\bdrop\s+(?:column\s+)?(?:if\s+exists\s+)?([a-z_][\w.]*)(?:\s|$)`)
	reIfNotExists = regexp.MustCompile(`\bif\s+not\s+exists\b`)
	reIfExists    = regexp.MustCompile(`\bif\s+exists\b`)
	reDDL         = regexp.MustCompile(`^(alter|create|drop)\b`)
	// reBatchWindow matches the drain's window relation where the SQL reads it, not
	// the word anywhere. A body that only says "batch" in a comment or in a value it
	// is writing does not go through the window, and a body that names it in another
	// case does: the case is folded before the reader sees it, and the literal's
	// contents are put away by the one reading the question is asked of
	// (migration.windowed).
	reBatchWindow = regexp.MustCompile(`\b(from|join|into|using)\s+batch\b`)
)

// rules is the table, in the order a reviewer reads it: the rewrites and locks
// first, then the execution modes, then the shapes of a data file. One file may
// break several rules; the first is what the operator is shown, because the fix
// of the first is usually the fix of the rest.
var rules = []rule{
	{name: "alter-column-type",
		does:      "changes a column's type, which rewrites the whole table under an ACCESS EXCLUSIVE lock while the application keeps trying to read it",
		instead:   "add the new column, backfill it in batches, write both for a release, then drop the old one in a contract file",
		exception: "allow=alter-column-type reason=<one sentence>",
		fire:      func(f *migrationText) bool { return f.rewritesAColumnType() }},
	{name: "add-column-not-null",
		does:      "adds a NOT NULL column with no DEFAULT, which rewrites the table and refuses every write while it does",
		instead:   "add the column nullable with a DEFAULT, or backfill it and add the constraint after VALIDATE",
		exception: "allow=add-column-not-null reason=<one sentence>",
		fire:      func(f *migrationText) bool { return f.addsANotNullColumn() }},
	{name: "index-not-concurrent",
		does:      "builds an index without CONCURRENTLY on a table this file does not create, which takes a SHARE lock that stops every writer for the length of the build",
		instead:   "build it in its own file with CONCURRENTLY and IF NOT EXISTS (that file carries autocommit=true), or ship the index in the file that creates the table",
		exception: "allow=index-not-concurrent reason=<one sentence>",
		fire:      func(f *migrationText) bool { return f.indexesANewTable() }},
	{name: "drop-column",
		does:      "drops a column, which takes a name away from the release that is running right now",
		instead:   "mark this file `-- pkit: phase=contract expand=<version>` and ship it in the release after the expansion that replaced it",
		exception: "allow=drop-column reason=<one sentence>, for a column no installation ever had rows in",
		fire:      func(f *migrationText) bool { return f.phase != phaseContract && f.dropsAColumn() }},
	{name: "index-concurrent-without-autocommit",
		does:    "runs CONCURRENTLY inside the transaction every migration file is applied in, which PostgreSQL refuses there (its error 25001)",
		instead: "add `-- pkit: autocommit=true`, which is the file shape that runs outside the transaction",
		fire:    func(f *migrationText) bool { return !f.autocommit && f.any(reConcurrently) }},
	{name: "autocommit-without-concurrently",
		does:    "takes itself out of the transaction that gives every other migration all-or-nothing, without a statement that needs it",
		instead: "delete the marker, or move the nontransactional statement into this file",
		fire:    func(f *migrationText) bool { return f.autocommit && !f.any(reConcurrently) }},
	{name: "autocommit-not-rerunnable",
		does:    "is a file that can commit its statement and still be re-run, because its statement refuses a second run",
		instead: "write CREATE INDEX CONCURRENTLY IF NOT EXISTS, or DROP INDEX CONCURRENTLY IF EXISTS",
		fire:    func(f *migrationText) bool { return f.autocommit && f.statementRefusesASecondRun() }},
	{name: "data-body-unbounded",
		does:      "is a data file whose body never reads the batch window, so the window cannot bound it and one statement walks the whole table",
		instead:   "put the body's rows through the window (WHERE … IN (SELECT … FROM batch)), or say why the body bounds itself",
		exception: "allow=data-body-unbounded reason=<one sentence>",
		fire: func(f *migrationText) bool {
			return f.phase == phaseData && !f.windowed
		}},
	{name: "data-with-ddl",
		does:    "changes the schema in the one file that runs outside a transaction and in pieces, where no DDL is atomic and none rolls back",
		instead: "split the file: the DDL is a schema file, the backfill is phase=data",
		fire: func(f *migrationText) bool {
			return f.phase == phaseData && f.anyStatement(reDDL)
		}},
	{name: "data-writes-outbox",
		does:      "writes the outbox from a batched backfill, which emits one event per row per attempt and replays every one of them on a resume",
		instead:   "write the rows here, then announce them in a step of its own that the relay can keep up with",
		exception: "allow=data-writes-outbox reason=<one sentence>",
		fire: func(f *migrationText) bool {
			return f.phase == phaseData && strings.Contains(f.body, "platformkit_outbox")
		}},
}

// unusedAllow is the last refusal, and the one a reviewer asks for: an exception
// marked on a file that broke nothing is a claim about a risk that is not there,
// and it outlives the sentence that justified it long after the reason was true.
const unusedAllow = "unused-allow"

// ruleNames and isRule make the table the interface the grammar refuses with, so
// that the list an operator is offered is the list the runner enforces.
func ruleNames() []string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.name)
	}
	return names
}

func isRule(name string) bool { return slices.Contains(ruleNames(), name) }

// checkRules reports the first rule this file breaks and does not except, and then
// an allow that excepted nothing. It reads the file's own text, so it runs before
// the runner connects: an invalid later file must not let an earlier one change
// the schema.
//
// A marker only excepts a rule that documents one. Four rules state no exception:
// what PostgreSQL refuses inside the transaction every other file runs in, what
// taking that transaction away costs, what a statement that cannot be re-run means
// in that mode, and what a data file cannot survive. A marker naming one of them is
// a bypass whose name hides it — unused-allow cannot see it, because the rule did
// fire — and the file would then answer in PostgreSQL's vocabulary rather than this
// table's.
func checkRules(m migration) error {
	f := newMigrationText(m)
	fired := f.fires()
	for _, r := range rules {
		if !slices.Contains(fired, r.name) {
			continue
		}
		if slices.Contains(m.allowedRule, r.name) {
			if r.exception == "" {
				return fmt.Errorf("rule %s: this file %s; %s — allow=%s excepts nothing here: this rule has no exception, so the marker is a bypass with a rule name on it",
					r.name, r.does, r.instead, r.name)
			}
			continue
		}
		return ruleError(r)
	}
	for _, allowed := range m.allowedRule {
		if !slices.Contains(fired, allowed) {
			return fmt.Errorf("rule %s: allow=%s excepts a rule this file does not break; delete the marker, or it will be read as true long after the sentence under it was",
				unusedAllow, allowed)
		}
	}
	return nil
}

func ruleError(r rule) error {
	msg := fmt.Errorf("rule %s: this file %s; %s", r.name, r.does, r.instead)
	if r.exception != "" {
		return fmt.Errorf("%w; or %s", msg, r.exception)
	}
	return msg
}

// newMigrationText takes the file as the rule table reads it: the one normalised
// body parseHeader produced, split into statements. It does not make a text of its
// own, because the drain asks the same body the same question (does this file go
// through the window) and a body judged by one reader and executed by another is a
// file whose execution was not the file that was reviewed. That one question is
// answered once, in migration.windowed, and both readers take the answer.
func newMigrationText(m migration) *migrationText {
	body := m.plain
	f := &migrationText{
		phase:      m.phase,
		autocommit: m.autocommit,
		body:       body,
		windowed:   m.windowed(),
		statements: splitStatements(body),
		created:    map[string]bool{},
	}
	for _, found := range reCreateTable.FindAllStringSubmatch(body, -1) {
		f.created[found[2]] = true
	}
	return f
}

func (f *migrationText) fires() []string {
	var fired []string
	for _, r := range rules {
		if r.fire(f) {
			fired = append(fired, r.name)
		}
	}
	return fired
}

func (f *migrationText) any(re *regexp.Regexp) bool { return re.MatchString(f.body) }

// rewritesAColumnType is the first rule's subject: a statement that changes a
// column's type. Both halves have to be in the one statement — an ALTER TABLE, and
// the clause that rewrites it — because a rule about a table rewrite must not fire
// over two statements that happen to sit near each other, and must not miss the
// spelling that leaves the keyword out.
func (f *migrationText) rewritesAColumnType() bool {
	for _, statement := range f.statements {
		statement = strings.TrimSpace(statement)
		if reAlterTable.MatchString(statement) && reColumnTypeClause.MatchString(statement) {
			return true
		}
	}
	return false
}

// addsANotNullColumn is the second rule's subject, read one column definition at a
// time for the same reason the type-change rule reads one statement at a time: the
// DEFAULT that excuses a NOT NULL column has to be that column's own. Reading the
// three words over the whole body lets any statement answer for any other — a
// DEFAULT on the column beside it, or none anywhere — and the rewrite then reaches
// PostgreSQL, where a 23502 stops it in the middle of a file whose earlier
// statements already applied. The `IS NOT NULL` a partial index carries in its WHERE
// clause is a predicate over rows, not a constraint this file adds, and is read the
// same way out.
func (f *migrationText) addsANotNullColumn() bool {
	for _, statement := range f.statements {
		for _, clause := range splitClauses(statement) {
			if addsANotNullDefinition(clause) {
				return true
			}
		}
	}
	return false
}

// notAColumn are the words that name an ADD of something other than a column. A
// CHECK or a UNIQUE constraint may say IS NOT NULL about rows that already exist
// without adding a column at all, and adding a column with no DEFAULT and no NOT
// NULL is the ordinary statement this rule exists to leave alone.
var notAColumn = map[string]bool{"constraint": true, "primary": true, "foreign": true, "unique": true, "check": true, "exclude": true}

// dropsAColumn is the fourth rule's subject, read one statement at a time for the same
// reason its two neighbours read one clause or one statement: the ALTER TABLE and the
// name it takes away have to be in the one statement, and a bare `DROP TABLE` or
// `DROP INDEX` is a different statement about a different object, whatever follows the
// word. PostgreSQL makes the COLUMN keyword optional here as it does for a type change,
// so `ALTER TABLE probe DROP b` is this rule's statement and not a spelling it may miss
// — the file that ships it takes the column away in the release running now, whatever
// the release was reviewed as.
func (f *migrationText) dropsAColumn() bool {
	for _, statement := range f.statements {
		if !reAlterTable.MatchString(strings.TrimSpace(statement)) {
			continue
		}
		for _, found := range reDropAction.FindAllStringSubmatch(statement, -1) {
			if !notADroppedColumn[found[1]] {
				return true
			}
		}
	}
	return false
}

// notADroppedColumn are the words ALTER TABLE puts after DROP for something that is not
// a column. Dropping a constraint's NOT NULL or DEFAULT is the ordinary shrink of a
// rule the release already carries, and none of them takes a column's name away.
var notADroppedColumn = map[string]bool{"constraint": true, "identity": true, "expression": true, "not": true, "null": true, "default": true}

// addsANotNullDefinition reads one comma-separated action of an ALTER TABLE: every
// column it adds, and each column's own definition — the text from its name to the
// end of the action, which is where its DEFAULT or NOT NULL would be.
func addsANotNullDefinition(clause string) bool {
	for _, found := range reAddColumnAction.FindAllStringSubmatchIndex(clause, -1) {
		// found[2:3] is the captured name, found[1] the end of the whole match: the
		// definition is everything the action says about that column.
		if notAColumn[clause[found[2]:found[3]]] {
			continue
		}
		if definition := clause[found[1]:]; reNotNull.MatchString(definition) && !reDefault.MatchString(definition) {
			return true
		}
	}
	return false
}

func (f *migrationText) anyStatement(re *regexp.Regexp) bool {
	for _, statement := range f.statements {
		if re.MatchString(strings.TrimSpace(statement)) {
			return true
		}
	}
	return false
}

// indexesANewTable is the shape of the ordinary case: the file that creates a
// table may index it, because nothing is reading it yet, and that is every
// module's first file. Anything else is a build on a table with readers.
func (f *migrationText) indexesANewTable() bool {
	for _, statement := range f.statements {
		statement = strings.TrimSpace(statement)
		if !reCreateIndex.MatchString(statement) || reConcurrently.MatchString(statement) {
			continue
		}
		target := reIndexTarget.FindStringSubmatch(statement)
		if target == nil || !f.created[target[1]] {
			return true
		}
	}
	return false
}

// statementRefusesASecondRun is the statement an autocommit file exists to run, in
// the form that does not survive its own success: the statement can commit while the
// version stays unapplied, and the next run has to be able to repeat it. Both of the
// statements the mode is for are checked, because both are in the rule's own remedy
// and only one of them is a CREATE.
func (f *migrationText) statementRefusesASecondRun() bool {
	for _, statement := range f.statements {
		statement = strings.TrimSpace(statement)
		if !reConcurrently.MatchString(statement) {
			continue
		}
		switch {
		case reCreateIndex.MatchString(statement) && !reIfNotExists.MatchString(statement),
			reDropIndex.MatchString(statement) && !reIfExists.MatchString(statement):
			return true
		}
	}
	return false
}

// blockCommentEnd is how many bytes a block comment starting at the front of text
// takes up, nesting included (`/* … /* … */ … */` is one comment to PostgreSQL, which
// is what makes `--` too narrow a rule for where commentary ends). An unterminated one
// runs to the end of the file, which is what the server reads there as well: it then
// refuses the file as a syntax error, and the guard has nothing to judge.
func blockCommentEnd(text string) int {
	for depth, i := 0, 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(text[i:], "*/"):
			if depth == 1 {
				return i + 2
			}
			depth--
			i += 2
		default:
			i++
		}
	}
	return len(text)
}

// dollarTag is the opening delimiter of a dollar-quoted value at the front of text —
// `$`, a tag, `$`, the tag optional for `$$` — or "" where none opens. The tag takes
// the characters a name takes and may not start with a digit, which is what keeps the
// drain's own `$1` and `LIMIT 100` from reading as the start of a value. The text
// arrives case-folded, so a tag whose two halves differ in case closes here where
// PostgreSQL would call the value unterminated: that file runs nowhere, and the fold
// is the same one every other word in it is read with.
func dollarTag(text string) string {
	if text == "" || text[0] != '$' {
		return ""
	}
	for i := 1; i < len(text); i++ {
		if c := text[i]; c == '$' {
			return text[:i+1]
		} else if !(c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' && i > 1) {
			return ""
		}
	}
	return ""
}

// isEscapeString is the E'…' prefix, which PostgreSQL reads as one token only where a
// name could not continue: the space in `time '3 days'` is what keeps a type name
// ending in e from swallowing the value after it, and the same space decides this.
func isEscapeString(text string, i int) bool {
	if i > 0 {
		switch c := text[i-1]; {
		case c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
			return false
		}
	}
	return (text[i] == 'e' || text[i] == 'E') && strings.HasPrefix(text[i+1:], "'")
}

// splitStatements breaks the body on semicolons outside quoted text. It is the
// same cut the measured rule floors were taken with, and it is not a parser: a
// body written inside a dollar-quoted function is split where its semicolons are,
// which is the false positive the exception marker exists for.
func splitStatements(body string) []string { return splitTopLevel(body, ';') }

// splitClauses breaks one statement on the commas between its ALTER TABLE actions,
// which is where one column definition ends and the next begins. Parentheses are
// counted because a DEFAULT is allowed to be a call: `DEFAULT coalesce(x, 0)` is one
// value and not two clauses.
func splitClauses(statement string) []string { return splitTopLevel(statement, ',') }

// splitTopLevel cuts text on a separator outside quoted text and outside
// parentheses, and drops the pieces that hold nothing.
func splitTopLevel(text string, sep rune) []string {
	var out, buf []string
	quoted, depth := false, 0
	for _, r := range text {
		switch {
		case r == '\'':
			quoted = !quoted
		case quoted:
		case r == '(':
			depth++
		case r == ')':
			depth = max(depth-1, 0)
		case r == sep && depth == 0:
			out = append(out, strings.Join(buf, ""))
			buf = nil
			continue
		}
		buf = append(buf, string(r))
	}
	if last := strings.Join(buf, ""); strings.TrimSpace(last) != "" {
		out = append(out, last)
	}
	return out
}

// scanSQL is the one reading the runner takes of a file's SQL: every comment is
// put away, and with blankLiterals set the contents of every string literal and
// quoted identifier go with them, which leaves the shape of the statements rather
// than the words inside them.
//
// Knowing where a comment starts means knowing where a literal does. One regexp over
// `--[^\n]*` took the rest of any line carrying two dashes out of the only text the
// rule table and the drain asked their questions of, so a body that reads the window
// after an apostrophe — `SET note = 'pending -- see the note' WHERE id IN (SELECT id
// FROM batch)` — was read as a body that never reads it, refused as unbounded, and
// then run unwrapped once its author obeyed the refusal's own remedy. The order of the
// two cases below is the other half of the same fact: a comment is consumed where it
// starts, quotes and all, so an apostrophe in commentary cannot open a literal and
// hide every statement after it — the mistake on the other side, and the one no
// exception marker would ever be blamed for.
//
// The same fact has three more spellings, and they fail in both directions at once, so
// all three are read here rather than worked around by asking the question of two
// texts: a `/* … */` runs across lines and nests, so the apostrophe in
// `/* it's a backfill */` is commentary and the statements after it are statements;
// `$tag$ … $tag$` is one value carrying apostrophes, semicolons and — as data — the
// words of the window, so it joins the other literals, put away for the shape and kept
// whole for the text; and `E'…'` takes a backslash, so it ends where its author ended
// it, which an ordinary `'…'` does not (a `\'` in one closes the literal and puts what
// follows outside it). A reading that missed any of them took a bounded body for an
// unbounded one, or the reverse, and the remedy the refusal offered moved the harm from
// one to the other.
//
// What is left unread is what a rule's exception exists for: the semicolons inside a
// dollar-quoted body still split statements, because that reading is splitTopLevel's
// and a body the runner cannot wrap is refused by the rule that says so rather than
// quietly run in pieces. The two readings differ in what they put away, never in where
// they think the text is.
func scanSQL(text string, blankLiterals bool) string {
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "--"):
			out.WriteByte(' ')
			if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
				i += end // the newline stays: it separated those two lines for the server as well
				continue
			}
			i = len(text)
		case strings.HasPrefix(text[i:], "/*"):
			// Consumed as commentary in both readings, and nested, which is what PostgreSQL
			// does with it: the apostrophe in `/* it's a backfill */` is not the opening of a
			// literal, and the newline it spans is not a line boundary.
			out.WriteByte(' ')
			i += blockCommentEnd(text[i:])
		case dollarTag(text[i:]) != "":
			tag := dollarTag(text[i:])
			end := len(text)
			if closed := strings.Index(text[i+len(tag):], tag); closed >= 0 {
				end = i + 2*len(tag) + closed
			}
			// One value, written with whatever quotes and semicolons it likes: joined to the
			// other literals, which means put away for the shape and kept whole for the text.
			if blankLiterals {
				out.WriteString(tag)
				out.WriteString(tag)
			} else {
				out.WriteString(text[i:end])
			}
			i = end
		case isEscapeString(text, i):
			start := i
			for i += 2; i < len(text); i++ {
				if text[i] == '\\' {
					i++ // the escape takes the next character, closing quote included
				} else if text[i] == '\'' {
					if i+1 < len(text) && text[i+1] == '\'' {
						i++ // '' is one quote written twice, as in any other literal
						continue
					}
					i++
					break
				}
			}
			if blankLiterals {
				out.WriteString("e''")
			} else {
				out.WriteString(text[start:i])
			}
		case text[i] == '\'' || text[i] == '"':
			quote, start := text[i], i
			for i++; i < len(text); i++ {
				if text[i] != quote {
					continue
				}
				if i+1 < len(text) && text[i+1] == quote {
					i++ // '' or "" is one quote written twice, not the end of the literal
					continue
				}
				i++
				break
			}
			if blankLiterals {
				out.WriteString(text[start : start+1])
				out.WriteByte(quote)
			} else {
				out.WriteString(text[start:i])
			}
		default:
			out.WriteByte(text[i])
			i++
		}
	}
	return out.String()
}
