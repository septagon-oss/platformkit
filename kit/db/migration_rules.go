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
//
// Reading a body *through* is not optional for a rule that anchors at a statement's
// front, and the cut alone does not give one: `splitStatements` breaks
// `DO $$ BEGIN ALTER TABLE … DROP COLUMN …; END $$` inside the value, so the piece still
// begins `do $$ begin alter table`, no rule is anchored there, and the dropped column
// reaches the server with nothing named — and no marker to write, because a rule that
// never fired cannot be excepted. So the four rules that read a statement read the
// statements inside a body beside the file's own (`dollarBodyStatements`), over the same
// predicates, and the two rules about `CONCURRENTLY` — which state no exception, so their
// reading may not over-reach in either direction — ask their question of the file's own
// SQL with the contents of every value put away, and of a stored body's SQL likewise
// (`migrationText.shape` and `migrationText.bodySQL`).
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
	// shape is the body with the contents of every literal and dollar-quoted value put
	// away, and bodySQL is the contents of those values read as the SQL they would run,
	// with their own literals and comments put away in turn. The two together answer
	// "does this file hold a CONCURRENTLY statement": the word in the file's own SQL is
	// the keyword, the word inside a value it is storing is data, and the word inside a
	// stored body is a statement the body will run someday. Both readings are asked of
	// the same scanner, and neither of them can be wrong about where a value ends — the
	// two rules that consult them state no exception, so their author has nothing to
	// answer with. See splitServerStatements for the same argument about the three
	// refusals that read a statement split.
	shape, bodySQL string
	windowed       bool
	statements     []string
	// serverStatements is the same body cut where PostgreSQL cuts it. Three questions are
	// asked of that cut rather than of `statements`, and they are the three this table
	// refuses with no `allow=` to answer: whether a data body is one statement (the
	// executor's), whether a statement begins with a DDL verb, and whether the file's own
	// statement can be re-run. See splitStatements and splitServerStatements for why one
	// file carries both cuts.
	serverStatements []string
	created          map[string]bool
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
	// reCreateTable is anchored because it decides an exemption, and the sentence that
	// grants it — "this file creates the table outright, so nothing is reading it yet" —
	// is a claim about a statement the file runs. `INSERT INTO probe (note) VALUES ('the
	// layout used to read CREATE TABLE probe (id bigint)')` writes a sentence about a
	// create; the build beside it is over the table the release is reading, and the words
	// in the value say nothing about who holds a SHARE lock.
	reCreateTable = regexp.MustCompile(`^create\s+table\s+(if\s+not\s+exists\s+)?(` + sqlName + `)`)
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
		fire:    func(f *migrationText) bool { return !f.autocommit && f.anySQL(reConcurrently) }},
	{name: "autocommit-without-concurrently",
		does:    "takes itself out of the transaction that gives every other migration all-or-nothing, without a statement that needs it",
		instead: "delete the marker, or move the nontransactional statement into this file",
		fire:    func(f *migrationText) bool { return f.autocommit && !f.holdsAConcurrentStatement() }},
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
		shape:      m.shape,
		bodySQL:    dollarBodySQL(body),
		windowed:   m.windowed(),
		// A body's statements are read from the inside and appended, so that a rule
		// anchored at a statement's front reads the same predicate over the same file
		// whoever wrapped it: see dollarBodyStatements.
		statements: append(splitStatements(body), dollarBodyStatements(body)...),
		// The drain asks "is this one statement?" of the same cut this file asks its two
		// unexceptable questions of, so the three are given the same cut to begin with.
		serverStatements: splitServerStatements(body),
		created:          map[string]bool{},
	}
	for _, statement := range f.statements {
		found := reCreateTable.FindStringSubmatch(strings.TrimSpace(statement))
		if found == nil {
			continue
		}
		// `IF NOT EXISTS` is the spelling that says the table may already be there, which
		// is the one case in which "nothing is reading it yet" is not a fact this file
		// states: on an installation that already has the table, the plain build beside it
		// is the SHARE lock over a table with readers the rule is about. Such a file is
		// therefore not known to have created the table, and the exemption is the
		// unconditional create's alone.
		//
		// The name is recorded with its quotes taken off, because it is the table that is
		// exempted and not the spelling: PostgreSQL reads `CREATE TABLE "probe"` as the one
		// table `CREATE INDEX … ON probe` names, and an exemption looked up by the
		// punctuation the two lines happened to use refuses the file that follows the rule's
		// own remedy.
		if found[1] != "" {
			continue
		}
		name, _ := sqlIdent(found[2])
		f.created[name] = true
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

// anySQL asks a rule about CONCURRENTLY the question that rule states: it reads the body
// with the contents of every
// literal and dollar-quoted value put away. It is the reading of the two rules that state no
// exception and ask after one word, and the put-away is the whole of it — outside a value,
// `concurrently` is the keyword and nothing else, so no statement shape has to be recognised
// to ask the question, and no value's prose can answer it. `migration.shape` is that text,
// the one `migration.windowed` already answers the window question of, and the reason is the
// same: a body whose only mention of a thing is data it is writing does not do that thing.
func (f *migrationText) anySQL(re *regexp.Regexp) bool { return re.MatchString(f.shape) }

// holdsAConcurrentStatement is the whole defence of `autocommit=true`: the file holds a
// statement that needs to run outside the transaction. It is asked of the file's own SQL and
// then of the SQL its stored bodies would run, because a build written inside a function
// this file creates is a statement that needs the mode when the function is called, and the
// marker is not the thing this rule exists to refuse. It is not asked of the contents of any
// other value, which is data: the file that writes the words "rebuild with CREATE INDEX
// CONCURRENTLY" into a column stores a sentence, and refusing it would refuse a file that
// cannot answer — `autocommit=true`, the rule's own remedy, is refused on the data file that
// spells it, and a marker for this rule is refused as a bypass.
func (f *migrationText) holdsAConcurrentStatement() bool {
	return f.anySQL(reConcurrently) || reConcurrently.MatchString(f.bodySQL)
}

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

// anyStatement asks whether any statement of the file matches, over the cut PostgreSQL
// makes. It is the reading `data-with-ddl` has to be asked of, because that rule states no
// exception: read from the inside, a `phase=data` body whose value carried `…; create table
// ghost …` was refused for changing the schema — the sentence described a DDL statement the
// file does not contain, and the `allow=` that would answer it is refused as a bypass.
//
// What a windowed body gains costs nothing: the kernel wraps it, so the only statement it can
// hold is the one the wrapper goes round. What it costs is an unwindowed one — a body excepted
// by `allow=data-body-unbounded`, the only data body that runs as written — whose
// `$tag$ … $tag$` value can hold a `DO`-shaped statement list the server would run: DDL after
// a semicolon inside such a value is no longer named here. It was named there by accident, the
// same read that took a JSON value for a schema change, and the `DO $$ … $$` whose DDL is the
// value's first words was never named at all. A data file that wants DDL and a body of its own
// has two files, which is what the refusal already tells its author to write.
func (f *migrationText) anyStatement(re *regexp.Regexp) bool {
	for _, statement := range f.serverStatements {
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
		// The target is looked up by the name it spells, not the spelling: the create and
		// the build may quote the table differently, and PostgreSQL reads both as one table.
		target := reIndexTarget.FindStringSubmatch(statement)
		if target == nil {
			return true
		}
		if name, _ := sqlIdent(target[1]); !f.created[name] {
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
//
// It reads the cut PostgreSQL makes for the same reason the two other refusals with no
// `allow=` do: a file that writes a function body mentioning `CREATE INDEX CONCURRENTLY` does
// not run that statement at all — measured, the server answers `CREATE INDEX CONCURRENTLY
// cannot be executed from a function`, and `DROP INDEX CONCURRENTLY` the same way — so a
// statement list inside one of this file's values can only ever be refused for a statement the
// file cannot run, and this rule states no exception to answer it with. A file whose *own*
// statement is the nontransactional one is refused exactly as it was.
func (f *migrationText) statementRefusesASecondRun() bool {
	for _, statement := range f.serverStatements {
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
//
// What that sentence licenses, and what it never did, is worth stating exactly: it
// licenses a *rule* to read a construct it may be wrong about, because the file can answer
// the rule with `allow=` and a sentence. It does not license a refusal the file cannot
// answer that way — see splitServerStatements for the three questions that have no marker,
// which are three this split used to answer.
func splitStatements(body string) []string { return splitTopLevel(body, ';', true) }

// splitServerStatements breaks the body where PostgreSQL breaks it. A semicolon inside a
// `$tag$ … $tag$` value is data that value is writing, and the value is one argument to the
// statement that carries it, so nothing after it is a statement of its own; outside the
// value, every semicolon cuts, so a statement following a function body is still read whole
// (the mistake this split must not repeat is counting the value's own quotes, which is what
// `sqlToken` decides once for both readers).
//
// Three questions are asked of this reading, and they are the three no `allow=` reaches an
// answer to: how many statements a body holds, which is the executor's question and decides
// whether the window can wrap it at all (kit/db/backfill.go, drain); whether a statement
// *begins* with a DDL verb, which is `data-with-ddl`'s; and whether the file's own statement
// can be re-run, which is `autocommit-not-rerunnable`'s. A refusal with no marker is not a
// judgement the author may contest, so it may not rest on a reading that is wrong about where
// a value ends: the file it refuses is unshippable rather than correctable, and the remedy the
// sentence names cannot be carried out. Everything else — the rewrites, the locks, the dropped
// column — keeps the reading above, where over-reading a construct costs an author a marker and
// its sentence rather than a release.
func splitServerStatements(body string) []string { return splitTopLevel(body, ';', false) }

// eachDollarBody calls body with the contents of every dollar-quoted value in text, the
// two delimiters gone. It is the one place the runner looks inside a value at what the
// value would run, and the two readers below share it because they share the question:
// where the value begins and ends is `dollarTag` and `dollarValueEnd`'s, the same pair
// `sqlToken` gives both of the file's readings.
//
// A value is read whether or not it is a routine body, because telling a `DO $$ … $$`
// block from a string a backfill is writing is a fact about the statement carrying it, and
// the guard does not parse. What that costs is stated where each reader is: a statement
// inside a value that is not a body has to reach the rule's anchor anyway, which is the one
// thing both readings refuse to do — read the words of a value as a statement — and the one
// unmarked question asked of a body is asked for the presence of a word rather than the
// shape of a statement, so no file is refused for a construct this reader invented.
func eachDollarBody(text string, body func(contents string)) {
	for i := 0; i < len(text); {
		tag := dollarTag(text[i:])
		if tag == "" {
			i++
			continue
		}
		end, open := dollarValueEnd(text, i), i+len(tag)
		closed := end - len(tag)
		if closed < open {
			// A value the file never closed: the server refuses the file for it
			// (`unterminated dollar-quoted string`), and there are no contents to read.
			closed = open
		}
		body(text[open:closed])
		i = end
	}
}

// dollarBodyStatements is the statements a file's dollar-quoted bodies hold, read from
// inside the value. A rule anchored at a statement's front — `^alter table`, `^create
// index` — cannot reach one of these any other way: `splitStatements` cuts the body at its
// own semicolons, which leaves the piece beginning `do $$ begin alter table …`, and no
// rule is anchored behind a dollar sign. That is not the approximation a marker exists
// for; it is a rule that never fires, and a rule that never fires cannot be excepted, so
// the file that wraps a `DROP COLUMN` in the conditional block every idempotent migration
// writes applied with nothing named and its own `allow=` refused as `unused-allow`. The
// harm is the rule's own — a name taken away from the release running now — and the only
// thing that hid it was a wrapper.
//
// What it over-reads is one thing and no more: a statement the body holds behind a test the
// running installation decides, which fires whatever the branch turns out to take. That is
// the over-reading a marker exists for, and `allow=drop-column` with a sentence is the answer
// the file writes. What it does not read is a value *inside* the body: the anchor still has
// to be reached, so a `RAISE NOTICE 'alter table probe drop column b'` and an `EXECUTE` of a
// string the body assembled keep their own verb at the front and fire nothing, exactly as the
// same words in a plain file's literal do. The reading stops at a value's opening quote on
// both sides of the boundary, which is the one rule these readings share.
func dollarBodyStatements(text string) []string {
	var out []string
	eachDollarBody(text, func(contents string) {
		for _, statement := range splitStatements(contents) {
			for round := 0; round < 8; round++ {
				// A nested block opens with its own `BEGIN` in front of the verb, so the run of
				// control words is taken until the front is a statement's; the bound is because
				// the alternation always leaves something behind on the pass that matches.
				shorter := plpgsqlOpener.ReplaceAllString(statement, "")
				if shorter == statement {
					break
				}
				statement = shorter
			}
			if statement = strings.TrimSpace(statement); statement != "" {
				out = append(out, statement)
			}
		}
	})
	return out
}

// dollarBodySQL is the contents of every dollar-quoted value with its own comments and
// literals put away — the SQL a stored body would run, in the same text `migration.shape`
// is the file's own SQL in. One word is read of it, so nothing about a statement's shape
// is decided here: inside the value, as outside one, a word that survives the put-away is
// a word the body says in SQL rather than one it stores.
func dollarBodySQL(text string) string {
	var out strings.Builder
	eachDollarBody(text, func(contents string) {
		out.WriteString(scanSQL(contents, true))
		out.WriteByte('\n')
	})
	return out.String()
}

// plpgsqlOpener matches the run of PL/pgSQL control words that stand in front of a body's
// statement: a block's `BEGIN`, a branch's test and the `THEN` that ends it, `ELSE`, a loop
// header, a label. They are the whole difference between `begin alter table probe drop
// column b` — which the rules read no action in — and the `alter table probe drop column b`
// PostgreSQL runs, and they are a closed list from one grammar rather than an approximation
// of the SQL one.
//
// What that list gives up is stated with the reading: a `CASE` arm (`WHEN … THEN`) is not in
// it, so a statement after one keeps its `WHEN` in front and is read as no statement at all,
// and a branch test whose own text carries `THEN` and a `BEGIN` ahead of the verb has its
// prefix taken one word further than the grammar would. The first is a miss with the shape of
// the finding this list exists for, and naming it is honest where growing the list on a guess
// would not be; the second only reads past one verb to another. Both are visible in the diff
// of the file that writes them, and a rule that reads a construct it may be wrong about is a
// rule `allow=` reaches.
var plpgsqlOpener = regexp.MustCompile(`(?s)^\s*(?:begin\b|if\b.*?\bthen\b|elsif\b.*?\bthen\b|else\b|while\b.*?\bloop\b|for\b.*?\bloop\b|foreach\b.*?\bloop\b|loop\b|<<.*?>>)\s*`)

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
