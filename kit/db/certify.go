package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

// A file that runs its statement outside a transaction is required to be
// re-runnable, and the rule is read as "the statement may be sent twice". That is
// true of CREATE INDEX CONCURRENTLY ... IF NOT EXISTS right up to the moment the
// first send is cancelled — by lock_timeout, which the run's own budget puts on
// the session before that statement runs, or by a client that gave up. PostgreSQL
// keeps the half-built index and marks it invalid; the second send then answers
// "relation already exists, skipping" and returns success. Certifying that answer
// writes a history row for a constraint the database does not enforce, and the
// first write that relies on it — an ON CONFLICT clause naming the index — answers
// 42P10 against an installation whose own ledger says the constraint holds.
//
// So the object the statement names is read back from the catalog before the file
// is recorded. A name that resolves to nothing is a statement that reported
// creating something and did not; a name that resolves to an invalid index is the
// cancelled build. Both get the same answer, once: the half-built object is dropped
// the way a concurrent build is dropped, and the file's statement runs again. What
// still is not valid afterwards refuses the file rather than recording it, because
// the alternative is a boot that reports success and a table whose writes fail from
// then on. A refused file records nothing, so the next boot finds it pending and
// meets the same object with the same repair.
//
// A file that builds several concurrent indexes in one statement needs each of them
// behind IF NOT EXISTS, which is what "re-runnable" asks of it anyway: the repair
// below re-runs the whole statement, so a second name that is already valid has to
// answer "skipping" rather than "already exists".
var concurrentIndex = regexp.MustCompile(`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)`)

// certifiableName is the only shape of name this check can read: `shape` puts a quoted
// identifier's contents away, and a name it has put away cannot be looked up in the
// catalog either. Skipping such a name is the behaviour before this file — an object
// nobody reads back — while refusing a file whose own statement ran would be a runner
// that breaks an installation over a name it cannot parse.
func certifiableName(name string) bool {
	return regexp.MustCompile(`^[a-z_][a-z0-9_$]*(\.[a-z_][a-z0-9_$]*)?$`).MatchString(name)
}

func (r *runner) certifyConcurrentIndex(ctx context.Context, migration migration) error {
	// The rule table's own reading, for the same reason it uses it: outside a value,
	// `concurrently` is the keyword and nothing else, so the file that writes a function
	// whose body builds an index is not a file that builds one (kit/db/
	// review8_an_autocommit_function_body_test.go). The put-away text is also where a
	// comment's mention of the keyword disappears.
	for _, found := range concurrentIndex.FindAllStringSubmatch(migration.shape, -1) {
		name := found[1]
		if !certifiableName(name) {
			continue
		}
		valid, exists, err := concurrentIndexState(ctx, r.conn, name)
		if err != nil {
			return err
		}
		if exists && valid {
			continue
		}
		if exists {
			// regclass::text is PostgreSQL's own quoting: the name reaches DROP
			// INDEX as one identifier of that schema, whatever it contains.
			var drop string
			if err := r.conn.QueryRowContext(ctx,
				`SELECT format('DROP INDEX CONCURRENTLY IF EXISTS %s', $1::regclass::text)`, name).Scan(&drop); err != nil {
				return fmt.Errorf("db: migrate: name the half-built index %s: %w", name, err)
			}
			if _, err := r.conn.ExecContext(ctx, drop); err != nil {
				return fmt.Errorf("db: migrate: drop the half-built index %s: %w", name, r.refused(err))
			}
		}
		if _, err := r.conn.ExecContext(ctx, migration.sql); err != nil {
			return fmt.Errorf("db: migrate: rebuild %s: %w", name, err)
		}
		if valid, exists, err = concurrentIndexState(ctx, r.conn, name); err != nil {
			return err
		}
		if !exists || !valid {
			return fmt.Errorf("db: migrate: %s leaves %s %s, which is not a constraint the database enforces: "+
				"nothing was recorded for this file and it may be run again",
				migration.name, name, map[bool]string{true: "invalid again", false: "absent"}[exists])
		}
	}
	return nil
}

// concurrentIndexState asks the catalog what the name the statement just used
// actually is: nothing, a build that was cancelled, or an index that holds.
func concurrentIndexState(ctx context.Context, conn *sql.Conn, name string) (valid, exists bool, err error) {
	if err := conn.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists); err != nil {
		return false, false, fmt.Errorf("db: migrate: look up %s: %w", name, err)
	}
	if !exists {
		return false, false, nil
	}
	err = conn.QueryRowContext(ctx,
		`SELECT i.indisvalid FROM pg_index i WHERE i.indexrelid = to_regclass($1)`, name).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) {
		return false, true, nil // a name that resolves to a table or a sequence, not an index
	}
	if err != nil {
		return false, true, fmt.Errorf("db: migrate: read the state of %s: %w", name, err)
	}
	return valid, true, nil
}
