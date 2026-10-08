package personaldata

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// Schema is what a module's own migrations say about its tables: which columns each
// one carries, and what each one is. It is read from SQL text rather than from a live
// database, which is what lets the refusal in Check reach a module that has never been
// migrated — and what makes the declaration a claim about bytes a reviewer can read.
type Schema map[string]map[string]Column

// Column is one column's type and the file that gave it that type.
type Column struct {
	Type string
	File string
}

// Held is one uuid column one table carries, with the file that gave it one. A
// refusal has to name the file: the person who has to fix this wrote that line, and
// nothing else will tell them which one.
type Held struct {
	Table, Column, File string
}

// ReadMigrations reads every `*.up.sql` at the root of files — the same set kit/db
// applies, from the same directory — and answers what tables and columns the module's
// SQL creates. A nil files is a module with no SQL of its own, which is where
// modules/admin and modules/web actually are.
func ReadMigrations(files fs.FS) (Schema, error) {
	schema := Schema{}
	if files == nil {
		return schema, nil
	}
	entries, err := fs.ReadDir(files, ".")
	if err != nil {
		return nil, fmt.Errorf("reading the module's migration directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		text, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if err := readSQL(schema, string(text), name); err != nil {
			return nil, err
		}
	}
	return schema, nil
}

// PersonColumns answers, in one order a refusal can be pinned against, every uuid
// column of every table this schema carries that is not one of the two columns every
// table in this repository has: the row's own id, and the tenant db.Tenant bounds it
// with. Neither is a fact about a person; nothing else is exempt.
func (s Schema) PersonColumns() []Held {
	var held []Held
	for _, table := range sortedKeys(map[string]map[string]Column(s)) {
		for _, column := range sortedKeys(map[string]Column(s[table])) {
			c := s[table][column]
			if contains(exemptColumns, column) || !strings.HasPrefix(c.Type, "uuid") {
				continue
			}
			held = append(held, Held{Table: table, Column: column, File: c.File})
		}
	}
	return held
}

// Tables answers the table names in one stable order.
func (s Schema) Tables() []string { return sortedKeys(map[string]map[string]Column(s)) }

// readSQL folds one migration file into the schema. It reads the spellings this
// repository's own SQL uses — CREATE TABLE, ALTER TABLE ADD COLUMN, and the
// IF NOT EXISTS and schema-qualified forms of both — over text with its comments
// already stripped, one column per line.
//
// It is a reader with a stated reach and not a Postgres parser: a table whose columns
// are all written on the line after its opening paren is outside it. What makes that
// safe is that the reader refuses rather than guesses. Every `CREATE TABLE` a file
// writes it has to have read, and every `ADD COLUMN` it has to have attached to an
// `ALTER TABLE` in the same file; otherwise ReadMigrations returns an error and Check
// refuses the module. A column that arrived in a shape this reader cannot read is
// therefore a red boot, and not a person column nobody had to declare. The three
// shapes outside the reach are each a case in personaldata_test.go.
func readSQL(schema Schema, text, file string) error {
	body := stripComments(text)

	read := 0
	for _, m := range createTable.FindAllStringSubmatchIndex(body, -1) {
		name := unquote(body[m[2]:m[3]])
		open := strings.IndexByte(body[m[0]:], '(') + m[0]
		inner, closed := matchingParen(body, open)
		if !closed {
			return fmt.Errorf("%s opens a CREATE TABLE for %q and never closes it", file, name)
		}
		if schema[name] == nil {
			schema[name] = map[string]Column{}
		}
		for _, definition := range splitDefinitions(inner) {
			columnName, typ, ok := columnDefinition(definition)
			if !ok {
				continue
			}
			schema[name][columnName] = Column{Type: typ, File: file}
		}
		read++
	}
	if wrote := len(createTableKeyword.FindAllStringIndex(body, -1)); wrote != read {
		return fmt.Errorf("%s writes %d CREATE TABLE statements and this reader read %d of them: "+
			"a table it cannot read is a table nothing can be declared about", file, wrote, read)
	}

	// Columns are read in the order the file writes them, so that the multi-line
	// spelling — one ALTER TABLE and three ADD COLUMN lines, the way modules/audit
	// writes 000035 — attaches every column to the table its statement named.
	table, added := "", 0
	for _, m := range statement.FindAllStringSubmatch(body, -1) {
		if m[1] != "" {
			table = unquote(m[2])
			continue
		}
		if table == "" {
			return fmt.Errorf("%s adds column %s before any ALTER TABLE names the table it belongs to", file, m[3])
		}
		if schema[table] == nil {
			schema[table] = map[string]Column{}
		}
		schema[table][unquote(m[3])] = Column{
			Type: strings.ToLower(strings.TrimSpace(m[4])),
			File: file,
		}
		added++
	}
	if wrote := len(addColumnKeyword.FindAllStringIndex(body, -1)); wrote != added {
		return fmt.Errorf("%s writes %d ADD COLUMN clauses and this reader read %d of them", file, wrote, added)
	}
	return nil
}

// stripComments removes `--` comments, the only comment form these files use, so that
// a sentence about a column cannot be read as one.
func stripComments(text string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if j := strings.Index(l, "--"); j >= 0 {
			lines[i] = l[:j]
		}
	}
	return strings.Join(lines, "\n")
}

var whitespace = regexp.MustCompile(`[\s]+`)

var (
	// `CREATE TABLE [IF NOT EXISTS] <name> (` — the body is read by matching the
	// parentheses after the name rather than by the regex, so that a table written
	// across thirty lines and a table written on one are the same statement to this
	// reader, and a CHECK constraint's own parentheses are depth and not the end.
	createTable = regexp.MustCompile(`(?is)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(["\w.]+)\s*\(`)
	// The two halves of one ALTER, in the order a file writes them: `ALTER TABLE
	// <name>` on its own, or `ADD COLUMN [IF NOT EXISTS] <column> <type>` with the type
	// run to the end of the line, because a following CONSTRAINT clause is not a type.
	statement = regexp.MustCompile(`(?is)(ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(["\w.]+))|ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?(["\w]+)\s+([^\n,]+)`)
	// What the file wrote, whether or not this reader could read it.
	createTableKeyword = regexp.MustCompile(`(?is)CREATE\s+TABLE`)
	addColumnKeyword   = regexp.MustCompile(`(?is)ADD\s+COLUMN`)
	// A column definition is a name and a type, however much whitespace, and
	// possibly nothing else at all — a constraint clause is the latter.
	columnDefinitionRe = regexp.MustCompile(`(?i)^([A-Za-z_][\w]*)(?:\s+([^\s].*))?$`)
)

// matchingParen returns what sits between the paren at `open` and its match.
func matchingParen(body string, open int) (string, bool) {
	if open < 0 || open >= len(body) || body[open] != '(' {
		return "", false
	}
	depth := 0
	for i := open; i < len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return body[open+1 : i], true
			}
		}
	}
	return "", false
}

// splitDefinitions cuts a CREATE TABLE body where its columns are cut: at a comma
// that is not inside a nested paren or a quoted default. It is the comma a parser
// would cut at, which is why the same reader takes the house's one-column-per-line
// files and a table written on one line without either being a special case.
func splitDefinitions(body string) []string {
	var out []string
	depth, start := 0, 0
	quoted := false
	for i := 0; i < len(body); i++ {
		switch c := body[i]; c {
		case '\'':
			quoted = !quoted
		case '(':
			if !quoted {
				depth++
			}
		case ')':
			if !quoted {
				depth--
			}
		case ',':
			if !quoted && depth == 0 {
				out = append(out, body[start:i])
				start = i + 1
			}
		}
	}
	return append(out, body[start:])
}

// columnDefinition reads one definition with its line breaks collapsed. A line
// opening on a constraint keyword names no column.
func columnDefinition(definition string) (name, typ string, ok bool) {
	s := strings.TrimSpace(whitespace.ReplaceAllString(definition, " "))
	if s == "" {
		return "", "", false
	}
	switch strings.ToUpper(strings.Split(s, " ")[0]) {
	case "CONSTRAINT", "PRIMARY", "UNIQUE", "FOREIGN", "CHECK", "EXCLUDE", "LIKE", "INHERITS", "PARTITION", "PERIOD":
		return "", "", false
	}
	m := columnDefinitionRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return unquote(m[1]), strings.ToLower(strings.TrimSpace(m[2])), true
}

// unquote drops a schema qualifier and the double quotes a name may wear, so that a
// module whose tables live in a schema of their own declares them by the same name it
// created them with.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return strings.Trim(s, `"`)
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
