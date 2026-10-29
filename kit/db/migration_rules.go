package db

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The rule table refuses unsafe migration statement shapes and names any
// available allowance. It reads lexical SQL rather than a full parse tree:
// anchored rules also inspect dollar bodies, while CONCURRENTLY checks inspect
// literal-free shapes. See README.md for refusals and scanner limits.
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

// quotedName accepts quoted identifiers with doubled-quote escapes; sqlName
// also accepts bare identifiers. Both forms include PostgreSQL's non-ASCII
// identifier bytes so a valid spelling cannot bypass a rule's target check.
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
	// (migration.windowed). The body that *binds* the name rather than reading it is a
	// different reading, of the body's own letters and its own grammar position, and it
	// lives beside the window that cannot run such a statement (backfill.go,
	// bindsWindowRelation).
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
