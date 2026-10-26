package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
)

// A file whose statement runs outside a transaction must be re-runnable, and
// "re-runnable" is not "re-sendable": a CREATE INDEX CONCURRENTLY cancelled by
// lock_timeout leaves an invalid index behind, and the second send answers
// "relation already exists, skipping". Certifying that answer would record a
// constraint the database does not enforce. So the object the statement names is
// read back from the catalog before the file is recorded, and a name that resolves
// to nothing or to an invalid index is dropped and rebuilt once. What is still not
// valid afterwards refuses the file, which records nothing and leaves it pending.
// A file that builds several concurrent indexes in one statement needs each of
// them behind IF NOT EXISTS, because the repair re-runs the whole statement. See
// README.md, "A re-runnable statement and the object it left".
var concurrentIndex = regexp.MustCompile(`(?is)\bCREATE\s+(?:UNIQUE\s+)?INDEX\s+CONCURRENTLY\s+(?:IF\s+NOT\s+EXISTS\s+)?(\S+)`)

// certifiableName is the only shape of name this check can read: `shape` puts a quoted
// identifier's contents away, and a name it has put away cannot be looked up in the
// catalog either. Skipping such a name is the behaviour before this file — an object
// nobody reads back — while refusing a file whose own statement ran would be a runner
// that breaks an installation over a name it cannot parse.
func certifiableName(name string) bool {
	return regexp.MustCompile(`^[a-z_][a-z0-9_$]*(\.[a-z_][a-z0-9_$]*)?$`).MatchString(name)
}

// sendRerunnable runs one autocommit file's statement and certifies what it named: the
// half of an autocommit apply that happens with the composition lock down, under the file's
// own lock (kit/db/migrate.go's holdFileLock).
//
// The statement goes out through execReaskable, which re-sends it while the server keeps
// choosing this session out of somebody else's cycle. The rule table's own demand that an
// autocommit statement be re-runnable is what makes that legal; the certification beside it
// is what makes it honest. A re-send that arrives after its own build was cancelled meets
// the `IF NOT EXISTS` the file has to carry and answers "relation already exists, skipping"
// over the invalid index the cancelled send left behind — which is the answer this file
// exists to refuse to record, so the repair case reads the object back. The repair needs no re-send of its own for the same reason: it
// already begins at the drop.
func (r *runner) sendRerunnable(ctx context.Context, migration migration) error {
	if err := r.execReaskable(ctx, migration.sql); err != nil {
		return err
	}
	// The repair waits for the same transactions this statement was released to wait for,
	// so the composition lock stays down for it too and comes back afterwards. Measured with
	// the lock held across the repair: `make check` picked this session out of that cycle five
	// times running in one file, which is every cycle repairAttempts holds and still no index.
	// ADR 0011 carries the deadlock DETAIL.
	return r.certifyConcurrentIndex(ctx, migration)
}

func (r *runner) certifyConcurrentIndex(ctx context.Context, migration migration) error {
	// The rule table's own reading, for the same reason it uses it: outside a value,
	// `concurrently` is the keyword and nothing else, so the file that writes a function
	// whose body builds an index is not a file that builds one. The put-away text is also where a
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
		if err := r.repairConcurrentIndex(ctx, migration, name, exists); err != nil {
			return err
		}
	}
	return nil
}

// repairAttempts is how many times one statement's repair is gone through before the
// file refuses: the first pass, plus the re-asks the drop half of it is allowed. It is
// the same bound for the same reason as the composition lock's: a cancellation breaks
// one cycle and costs no wait of its own, and a database whose next wait joins another
// cycle is a state a boot should name rather than sit in.
const repairAttempts = reasksAfterDeadlock + 1

// repairConcurrentIndex drops the half-built object, runs the file's statement again and
// reads the object back — and, when the server picks this session out of another session's
// cycle while it is doing that, starts that whole sequence again from the drop, while
// repairAttempts holds. The cycle is the unit of the retry because the two statements are
// not re-sendable in the same way: see this file's opening.
// None of it happens under the composition lock — see sendRerunnable — and none of it
// beside another boot repairing the same object, because this file runs under its own lock
// (kit/db/migrate.go's holdFileLock): holding the composition lock while waiting for a
// transaction that is waiting for it is the cycle itself. An answer that is not the
// detector's pick comes back as it arrived, and what the cycles cannot settle refuses the
// file.
func (r *runner) repairConcurrentIndex(ctx context.Context, migration migration, name string, exists bool) error {
	for attempt := 1; ; attempt++ {
		if exists {
			// regclass::text is PostgreSQL's own quoting: the name reaches DROP
			// INDEX as one identifier of that schema, whatever it contains.
			var drop string
			if err := r.conn.QueryRowContext(ctx,
				`SELECT format('DROP INDEX CONCURRENTLY IF EXISTS %s', $1::regclass::text)`, name).Scan(&drop); err != nil {
				return fmt.Errorf("db: migrate: name the half-built index %s: %w", name, err)
			}
			if err := r.execReaskable(ctx, drop); err != nil {
				return fmt.Errorf("db: migrate: drop the half-built index %s: %w", name, r.refused(err))
			}
		}
		_, err := r.conn.ExecContext(ctx, migration.sql)
		if err != nil && !deadlockVictim(err) {
			return fmt.Errorf("db: migrate: rebuild %s: %w", name, err)
		}
		valid, still, stateErr := concurrentIndexState(ctx, r.conn, name)
		if stateErr != nil {
			return stateErr
		}
		if valid {
			return nil
		}
		if deadlockVictim(err) && attempt < repairAttempts {
			exists = still
			continue
		}
		refusal := fmt.Sprintf("db: migrate: %s leaves %s %s, which is not a constraint the database enforces: "+
			"nothing was recorded for this file and it may be run again",
			migration.name, name, map[bool]string{true: "invalid again", false: "absent"}[still])
		if err != nil {
			return fmt.Errorf("%s: %w", refusal, err) // the server's own words for the pick that used the last cycle
		}
		return errors.New(refusal)
	}
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
