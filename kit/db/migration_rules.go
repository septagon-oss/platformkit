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
// split, lower-cased, the tables this very file creates outright known by name, and the window
// question already answered for the whole file rather than once per rule.
type migrationText struct {
	phase      string
	autocommit bool
	body       string
	windowed   bool
	statements []string
	created    map[string]bool
}

// quotedName is the source for one identifier written the only way PostgreSQL takes
// for a name that cannot be written bare — a reserved word, or a name created inside
// double quotes — which is `"…"`, with `""` for one quote written twice. sqlName is a
// name in either spelling.
//
// Every rule that finds its operation by reading the name an action carries has to
// read both spellings. A capture that starts `[a-z_]` is the spelling of a bare
// identifier, and `ALTER TABLE probe DROP "order"` is not a spelling an author chose
// over another: it is the only one that parses. A rule that read only the bare form
// would see no action at all for exactly the columns that cannot be named any other
// way — and the harm is its own rule's, applied, with the version in the ledger and no
// rule named. The same reason makes the exemption below read quoted names: the file
// that creates `"probe"` and indexes `"probe"` creates what it indexes.
//
// The bare spelling has to take the database's own letters as well. PostgreSQL's
// `ident_start` is `[A-Za-z_\200-\377]`, and it folds only ASCII when it down-cases an
// unquoted identifier, so a name carrying a c-cedilla and an a-tilde is legal written
// bare in the UTF-8 database every installation of this kernel runs under:
// `ALTER TABLE probe DROP atualizacao` (that name, spelled with those two letters) is a
// name the server takes and a name the reader stopped at — the same mis-capture as the
// quoted one, failing the same way: no rule fires, so no marker is ever offered, and the
// rewrite the rule is about reaches the server with the version in the ledger. A byte ≥
// 0x80 is therefore a name character here for dollarTag's own reason: the safe side of
// this guess is the side that reads too little.
const (
	quotedName = `"(?:[^"]|"")*"`
	// nameStart and nameBody are `ident_start` and `ident_cont` for a name arriving in
	// the case-folded text: the ASCII upper case is what the fold removed, the high bytes
	// are what the database's encoding adds, and the `.` is the one of a qualified name.
	// columnName is the same with the `$` PostgreSQL takes inside a column name, which
	// only the type-change rule reads.
	nameStart  = `[a-z_\x{0080}-\x{10FFFF}]`
	nameBody   = `[\w.\x{0080}-\x{10FFFF}]`
	sqlName    = `(?:` + nameStart + nameBody + `*|` + quotedName + `)`
	columnName = `(?:` + nameStart + `[\w.$\x{0080}-\x{10FFFF}]*|` + quotedName + `)`
)

var (
	// reAlterTable and reColumnTypeClause are the two halves of the statement the
	// first rule is about. PostgreSQL makes the COLUMN keyword optional and spells
	// the clause either `TYPE` or `SET DATA TYPE`; every spelling of it rewrites the
	// whole table under ACCESS EXCLUSIVE, so the predicate reads the clause inside an
	// ALTER TABLE statement rather than one spelling of the keyword — of the keyword,
	// of the column's name, and of the two ways that name may be written.
	reAlterTable       = regexp.MustCompile(`^alter\s+table\b`)
	reColumnTypeClause = regexp.MustCompile(`\balter\s+(column\s+)?(?:` + columnName + `)\s+(set\s+data\s+)?type\b`)
	// reAddColumnAction is `ADD [COLUMN] <name>` in either spelling PostgreSQL
	// takes. The name is captured because the word after ADD says what is being
	// added: `ADD CONSTRAINT x CHECK (… IS NOT NULL)` adds a table constraint, not a
	// NOT NULL column, and reads as neither.
	reAddColumnAction = regexp.MustCompile(`\badd\s+(?:column\s+)?(` + sqlName + `)(?:\s|$)`)
	reNotNull         = regexp.MustCompile(`\bnot\s+null\b`)
	reDefault         = regexp.MustCompile(`\bdefault\b`)
	reCreateIndex     = regexp.MustCompile(`^create\s+(unique\s+)?index\b`)
	reDropIndex       = regexp.MustCompile(`^drop\s+index\b`)
	reConcurrently    = regexp.MustCompile(`\bconcurrently\b`)
	reIndexTarget     = regexp.MustCompile(`\bon\s+(` + sqlName + `)`)
	reCreateTable     = regexp.MustCompile(`create\s+table\s+(if\s+not\s+exists\s+)?(` + sqlName + `)`)
	// reDropAction is `DROP [COLUMN] <name>` in either spelling PostgreSQL takes, the
	// keyword being as optional here as it is in the two ALTER TABLE actions above. The
	// name is captured because the word after DROP says what is being dropped, and only
	// a column takes a name away from the running release.
	reDropAction  = regexp.MustCompile(`\bdrop\s+(?:column\s+)?(?:if\s+exists\s+)?(` + sqlName + `)(?:\s|$)`)
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
		// `IF NOT EXISTS` is the spelling that says the table may already be there, which
		// is the one case in which "nothing is reading it yet" is not a fact this file
		// states: on an installation that already has the table, the plain build beside it
		// is the SHARE lock over a table with readers the rule is about. Such a file is
		// therefore not known to have created the table, and the exemption is the
		// unconditional create's alone.
		if found[1] == "" {
			f.created[found[2]] = true
		}
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
// the release was reviewed as. The name itself is read in either spelling PostgreSQL
// takes, and a name that arrives quoted is a column whatever it spells: the keyword
// exemption below is only ever the bare word's.
func (f *migrationText) dropsAColumn() bool {
	for _, statement := range f.statements {
		if !reAlterTable.MatchString(strings.TrimSpace(statement)) {
			continue
		}
		for _, found := range reDropAction.FindAllStringSubmatch(statement, -1) {
			if name, quoted := sqlIdent(found[1]); quoted || !notADroppedColumn[name] {
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

// sqlIdent is the name a rule read, with its quotes taken off, and whether they were
// there. The difference is not decoration: PostgreSQL reads a quoted name as an
// identifier and never as the keyword it also spells, so `ALTER TABLE probe DROP
// "constraint"` drops a column called constraint, and the exemption that lets the
// keyword alone through speaks for no such name. `""` is one quote written twice.
func sqlIdent(found string) (name string, quoted bool) {
	if len(found) > 1 && strings.HasPrefix(found, `"`) && strings.HasSuffix(found, `"`) {
		return strings.ReplaceAll(found[1:len(found)-1], `""`, `"`), true
	}
	return found, false
}

// addsANotNullDefinition reads one comma-separated action of an ALTER TABLE: every
// column it adds, and each column's own definition — the text from its name to the
// end of the action, which is where its DEFAULT or NOT NULL would be.
func addsANotNullDefinition(clause string) bool {
	for _, found := range reAddColumnAction.FindAllStringSubmatchIndex(clause, -1) {
		// found[2:3] is the captured name, found[1] the end of the whole match: the
		// definition is everything the action says about that column.
		if name, quoted := sqlIdent(clause[found[2]:found[3]]); !quoted && notAColumn[name] {
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
// module's first file. Anything else is a build on a table with readers — including
// the file whose own create was conditional, because `CREATE TABLE IF NOT EXISTS` is
// the spelling that says the table may already be there, and on such an installation
// this file's plain build is the one the rule is about.
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
// drain's own `$1` and `LIMIT 100` from reading as the start of a value.
//
// "The characters a name takes" is PostgreSQL's own domain, and it is a byte rule, not
// an ASCII one: a tag may carry any letter of the database's encoding, so in a UTF-8
// database `$atualização$` opens one value exactly as `$tag$` does. Answering "not a
// tag" for a byte above ASCII would be the unsafe direction of a guess — the value's
// contents then stay in the text the window question is asked of, and a body whose only
// `from batch` is data it is writing reads as a bounded one and is wrapped, running its
// whole-table statement once per window over every row. A byte ≥ 0x80 is therefore a tag
// character here (and a `$` still closes the tag, which is why `$a$b$c$` is the tag
// `$a$` here and to the server).
//
// The text arrives case-folded, so a tag whose two halves differ in case closes here
// where PostgreSQL would call the value unterminated: that file runs nowhere — measured,
// `unterminated dollar-quoted string` — and the fold is the same one every other word in
// it is read with.
func dollarTag(text string) string {
	if text == "" || text[0] != '$' {
		return ""
	}
	for i := 1; i < len(text); i++ {
		switch c := text[i]; {
		case c == '$':
			return text[:i+1]
		case !isIdentByte(c) || i == 1 && !isIdentStart(c):
			return ""
		}
	}
	return ""
}

// dollarValueEnd is the index one dollar-quoted value opening at i of text ends at —
// past its closing tag, or at the end of the text where no closing tag ever comes. The
// second answer is what the server is left with too, and it refuses the file for it
// (`unterminated dollar-quoted string`) rather than read the rest of the file as data.
func dollarValueEnd(text string, i int) int {
	tag := dollarTag(text[i:])
	if closed := strings.Index(text[i+len(tag):], tag); closed >= 0 {
		return i + 2*len(tag) + closed
	}
	return len(text)
}

// isIdentStart and isIdentByte are PostgreSQL's `ident_start` and `ident_cont`: an
// underscore, an ASCII letter, any byte ≥ 0x80 (a name takes the database's own letters,
// which is the argument sqlName and dollarTag each make for themselves), and — past the
// first byte of the name — a digit.
func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentByte(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// isEscapeString is the E'…' prefix, which PostgreSQL reads as one token only where a
// name could not continue: the space in `time '3 days'` is what keeps a type name
// ending in e from swallowing the value after it, and the same space decides this —
// tested against the same byte the two name readers test, because the e at the end of an
// accented name is inside a name and not the prefix of a value.
func isEscapeString(text string, i int) bool {
	if i > 0 && isIdentByte(text[i-1]) {
		return false
	}
	return (text[i] == 'e' || text[i] == 'E') && strings.HasPrefix(text[i+1:], "'")
}

// splitStatements breaks the body on semicolons outside quoted text. It is the
// same cut the measured rule floors were taken with, and it is not a parser: a
// body written inside a dollar-quoted function is split where its semicolons are,
// which is the false positive the exception marker exists for. Nothing else about such a
// body is read at all: see splitTopLevel.
func splitStatements(body string) []string { return splitTopLevel(body, ';', true) }

// splitClauses breaks one statement on the commas between its ALTER TABLE actions,
// which is where one column definition ends and the next begins. Parentheses are
// counted because a DEFAULT is allowed to be a call: `DEFAULT coalesce(x, 0)` is one
// value and not two clauses, and for the same reason a dollar-quoted body is not cut here
// at all: the comma this splitter is for separates two SQL actions, and a comma inside one
// value separates nothing.
func splitClauses(statement string) []string { return splitTopLevel(statement, ',', false) }

// splitTopLevel cuts text on a separator outside quoted text and outside
// parentheses, and drops the pieces that hold nothing.
//
// Both quote pairs count, because the separator can sit inside either of them. The `'…'`
// half is the case this function was written for. The `"…"` half is a *name*: a column
// called `a,b` or `a;b` exists only written quoted, and a cut at its punctuation leaves a
// rule reading no action in either half — measured on the previous revision,
// `ALTER TABLE probe ADD "a,b" text NOT NULL` and `ALTER TABLE probe DROP "a;b"` each
// applied with nothing named, which is the same fault finding the name captures now carry
// one level earlier. A quoted name is one name here exactly as the server reads it.
//
// Where a construct ends is the decision scanSQL makes, and both readers take it from
// sqlToken, because a separator inside any construct is data: an `E'…'` ending at the
// escape before its own closing quote, and an apostrophe inside a value, each cut a file in
// half exactly as a counted quote does.
//
// bodyCuts says whether the separator inside a `$tag$ … $tag$` value cuts, which is the one
// way the two callers differ and the one thing this function is not uniform about, because
// the two questions are different: the statement splitter has always read a function body
// from the inside, and that is the documented approximation whose false positive a marker
// excepts, while the clause splitter asks where one column definition ends. What neither
// may do is count the value's quotes and parentheses. Reading them was not conservatism: one
// lone `"` in a function body left the splitter certain the rest of the file was a name, no
// later semicolon cut, and the two rules anchored at the front of a statement read no action
// in the `ALTER TABLE` after the body — a false negative, with no rule to name and therefore
// no marker to except. An unbalanced `(` moved the same boundary the other way. A body's
// bytes are copied whole, and only its separators are read.
//
// Measured against every `.up.sql` in this repository, the normalised text, the shape and
// the statement split are byte-identical to the previous revision's; only a file that put a
// lone quote or an unbalanced parenthesis inside a dollar body reads differently, and no file
// anyone has shipped does.
func splitTopLevel(text string, sep byte, bodyCuts bool) []string {
	var out []string
	var buf []byte
	depth := 0
	for i := 0; i < len(text); {
		if tag := dollarTag(text[i:]); tag != "" {
			end := dollarValueEnd(text, i)
			for bodyCuts && i < end {
				if text[i] == sep {
					out = append(out, string(buf))
					buf = buf[:0]
				} else {
					buf = append(buf, text[i])
				}
				i++
			}
			buf = append(buf, text[i:end]...)
			i = end
			continue
		}
		if width := sqlToken(text, i); width > 0 {
			buf = append(buf, text[i:i+width]...)
			i += width
			continue
		}
		switch c := text[i]; {
		case c == '(':
			depth++
			buf = append(buf, c)
		case c == ')':
			depth = max(depth-1, 0)
			buf = append(buf, c)
		case c == sep && depth == 0:
			out = append(out, string(buf))
			buf = buf[:0]
			i++
			continue
		default:
			buf = append(buf, c)
		}
		i++
	}
	if last := string(buf); strings.TrimSpace(last) != "" {
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
// they think the text is — and that is a property rather than an intention, because both
// readers ask sqlToken where the construct in front of them ends.
func scanSQL(text string, blankLiterals bool) string {
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if width := sqlToken(text, i); width > 0 {
			token := text[i : i+width]
			i += width
			// Commentary is put away in both readings — that is what "the guard reads the
			// file with its comments gone" means, and the statement the rules anchor on
			// (`^alter table`, `^create index`) starts where the comment above it ends —
			// while the contents of a value are put away only for the shape, which is the
			// one question asked of text with the words taken out of it.
			if blankLiterals || strings.HasPrefix(token, "--") || strings.HasPrefix(token, "/*") {
				out.WriteString(blankSQLToken(token))
				continue
			}
			out.WriteString(token)
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// sqlToken is how many bytes the comment, dollar-quoted value, escape string, string
// literal or quoted name opening at index i of text takes up, and 0 where none of them
// opens. It is the one place the runner decides where such a construct ends, and it is
// shared because two readers have to agree about it: scanSQL reads a construct to put it
// away (or to keep it whole), and splitTopLevel reads it to know which of its bytes may
// cut a statement — a separator inside any of them is data. A width rather than an end
// index, so the caller keeps walking in the units the text arrives in, and 0 for the
// ordinary bytes, which no caller wants to copy twice over.
func sqlToken(text string, i int) int {
	switch {
	case strings.HasPrefix(text[i:], "--"):
		// The newline stays outside the token: it separated those two lines for the server
		// as well, so the reader that puts the commentary away keeps the line break.
		if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
			return end
		}
		return len(text) - i
	case strings.HasPrefix(text[i:], "/*"):
		// Consumed as commentary in both readings, and nested, which is what PostgreSQL does
		// with it: the apostrophe in `/* it's a backfill */` is not the opening of a literal,
		// and the newline it spans is not a line boundary.
		return blockCommentEnd(text[i:])
	case dollarTag(text[i:]) != "":
		// One value, written with whatever quotes and semicolons it likes: joined to the
		// other literals, which means put away for the shape and kept whole for the text.
		return dollarValueEnd(text, i) - i
	case isEscapeString(text, i):
		for j := i + 2; j < len(text); j++ {
			if text[j] == '\\' {
				j++ // the escape takes the next character, closing quote included
			} else if text[j] == '\'' {
				if j+1 < len(text) && text[j+1] == '\'' {
					j++ // '' is one quote written twice, as in any other literal
					continue
				}
				return j + 1 - i
			}
		}
		return len(text) - i
	case text[i] == '\'' || text[i] == '"':
		quote := text[i]
		for j := i + 1; j < len(text); j++ {
			if text[j] != quote {
				continue
			}
			if j+1 < len(text) && text[j+1] == quote {
				j++ // '' or "" is one quote written twice, not the end of the literal
				continue
			}
			return j + 1 - i
		}
		return len(text) - i
	}
	return 0
}

// blankSQLToken is what one sqlToken leaves in the shape: commentary is put away, and so
// is the content of every value, which is data and not the words of a statement. The
// delimiter stays, because an empty quoted value, an empty escape string and a tag
// written twice are what a value looks like from the outside, and the tag is the only
// part of a dollar-quoted body that is SQL rather than data.
func blankSQLToken(token string) string {
	switch {
	case strings.HasPrefix(token, "--") || strings.HasPrefix(token, "/*"):
		return " "
	case token[0] == '$':
		return dollarTag(token) + dollarTag(token)
	case isEscapeString(token, 0):
		return "e''"
	default:
		return token[:1] + token[:1]
	}
}
