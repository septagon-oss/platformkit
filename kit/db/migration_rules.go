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
// split, lower-cased, and the tables this very file creates known by name.
type migrationText struct {
	phase      string
	autocommit bool
	body       string
	statements []string
	created    map[string]bool
}

var (
	reAlterColumnType = regexp.MustCompile(`alter\s+table\b.*alter\s+column\b.*\b(type|set\s+data\s+type)\b`)
	reAddColumn       = regexp.MustCompile(`\badd\s+column\b`)
	reNotNull         = regexp.MustCompile(`\bnot\s+null\b`)
	reDefault         = regexp.MustCompile(`\bdefault\b`)
	reCreateIndex     = regexp.MustCompile(`^create\s+(unique\s+)?index\b`)
	reConcurrently    = regexp.MustCompile(`\bconcurrently\b`)
	reIndexTarget     = regexp.MustCompile(`\bon\s+([a-z_][\w.]*)`)
	reCreateTable     = regexp.MustCompile(`create\s+table\s+(if\s+not\s+exists\s+)?([a-z_][\w.]*)`)
	reDropColumn      = regexp.MustCompile(`\bdrop\s+column\b`)
	reIfNotExists     = regexp.MustCompile(`\bif\s+not\s+exists\b`)
	reIfExists        = regexp.MustCompile(`\bif\s+exists\b`)
	reBatchWindow     = regexp.MustCompile(`\bbatch\b`)
	reDDL             = regexp.MustCompile(`^(alter|create|drop)\b`)
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
		fire:      func(f *migrationText) bool { return f.any(reAlterColumnType) }},
	{name: "add-column-not-null",
		does:      "adds a NOT NULL column with no DEFAULT, which rewrites the table and refuses every write while it does",
		instead:   "add the column nullable with a DEFAULT, or backfill it and add the constraint after VALIDATE",
		exception: "allow=add-column-not-null reason=<one sentence>",
		fire:      func(f *migrationText) bool { return f.any(reAddColumn) && f.any(reNotNull) && !f.any(reDefault) }},
	{name: "index-not-concurrent",
		does:      "builds an index without CONCURRENTLY on a table this file does not create, which takes a SHARE lock that stops every writer for the length of the build",
		instead:   "build it in its own file with CONCURRENTLY and IF NOT EXISTS (that file carries autocommit=true), or ship the index in the file that creates the table",
		exception: "allow=index-not-concurrent reason=<one sentence>",
		fire:      func(f *migrationText) bool { return f.indexesANewTable() }},
	{name: "drop-column",
		does:      "drops a column, which takes a name away from the release that is running right now",
		instead:   "mark this file `-- pkit: phase=contract expand=<version>` and ship it in the release after the expansion that replaced it",
		exception: "allow=drop-column reason=<one sentence>, for a column no installation ever had rows in",
		fire:      func(f *migrationText) bool { return f.phase != phaseContract && f.any(reDropColumn) }},
	{name: "index-concurrent-without-autocommit",
		does:    "runs CONCURRENTLY inside the transaction every migration file is applied in, which PostgreSQL refuses (SQLSTATE 25001)",
		instead: "add `-- pkit: autocommit=true`, which is the file shape that runs outside the transaction",
		fire:    func(f *migrationText) bool { return !f.autocommit && f.any(reConcurrently) }},
	{name: "autocommit-without-concurrently",
		does:    "takes itself out of the transaction that gives every other migration all-or-nothing, without a statement that needs it",
		instead: "delete the marker, or move the nontransactional statement into this file",
		fire:    func(f *migrationText) bool { return f.autocommit && !f.any(reConcurrently) }},
	{name: "autocommit-not-rerunnable",
		does:    "is a file that can commit its statement and still be re-run, because its statement refuses a second run",
		instead: "write CREATE INDEX CONCURRENTLY IF NOT EXISTS, or DROP INDEX CONCURRENTLY IF EXISTS",
		fire:    func(f *migrationText) bool { return f.autocommit && f.indexWithoutIFNotExists() }},
	{name: "data-body-unbounded",
		does:      "is a data file whose body never mentions batch, so the window cannot bound it and one statement walks the whole table",
		instead:   "put the body's rows through the window (WHERE … IN (SELECT … FROM batch)), or say why the body bounds itself",
		exception: "allow=data-body-unbounded reason=<one sentence>",
		fire: func(f *migrationText) bool {
			return f.phase == phaseData && !reBatchWindow.MatchString(f.body)
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
func checkRules(m migration) error {
	f := newMigrationText(m)
	fired := f.fires()
	for _, r := range rules {
		if slices.Contains(fired, r.name) && !slices.Contains(m.allowedRule, r.name) {
			return ruleError(r)
		}
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

// newMigrationText strips the comments and splits the body the way every rule
// reads it.
func newMigrationText(m migration) *migrationText {
	body := stripSQLComments(strings.ToLower(m.body))
	f := &migrationText{
		phase:      m.phase,
		autocommit: m.autocommit,
		body:       body,
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

// indexWithoutIFNotExists is the one statement an autocommit file exists to run,
// and the form that survives its own success: the statement can commit while the
// version stays unapplied, and the next run has to be able to repeat it.
func (f *migrationText) indexWithoutIFNotExists() bool {
	for _, statement := range f.statements {
		statement = strings.TrimSpace(statement)
		if !reCreateIndex.MatchString(statement) || !reConcurrently.MatchString(statement) {
			continue
		}
		if !reIfNotExists.MatchString(statement) && !reIfExists.MatchString(statement) {
			return true
		}
	}
	return false
}

// splitStatements breaks the body on semicolons outside quoted text. It is the
// same cut the measured rule floors were taken with, and it is not a parser: a
// body written inside a dollar-quoted function is split where its semicolons are,
// which is the false positive the exception marker exists for.
func splitStatements(body string) []string {
	var out, buf []string
	quoted := false
	for _, r := range body {
		switch {
		case r == '\'':
			quoted = !quoted
		case r == ';' && !quoted:
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

var reLineComment = regexp.MustCompile(`--[^\n]*`)

func stripSQLComments(text string) string { return reLineComment.ReplaceAllString(text, " ") }
