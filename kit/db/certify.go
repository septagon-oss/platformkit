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
//
// The drop is sent through execReaskable, because being picked out of another session's
// cycle costs that statement nothing: cancelled or not, the object is gone afterwards,
// and `DROP INDEX CONCURRENTLY IF EXISTS` says so twice. The rebuild is not, and that
// is the one asymmetry in the pair. A concurrent build the deadlock detector cancels
// does not leave nothing behind — it leaves the half-built, invalid index, which is what
// this file exists to notice — so re-sending the build on its own meets the `IF NOT
// EXISTS` the file has to carry and answers "relation already exists, skipping": the
// statement doing nothing and reporting that it applied, which is the answer the check
// below refuses to record. Being picked out of a cycle therefore costs the repair a
// whole cycle, drop first, and not a re-send. Measured twice in this branch: a rebuild
// of this repair answered 40P01 under `make check` and lost the file for that boot, and
// the first cut of the cure, which re-sent the rebuild the way it re-sent the drop,
// lost the file the same way with a different sentence —
//
//	leaves build_probe_sent_once invalid again, which is not a constraint the database enforces
//
// because the re-send had reported success over the index the cancelled send left.
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
// reads the object back — and, when the server picks this session out of another
// session's cycle while it is doing that, starts that whole sequence again from the
// drop, while repairAttempts holds. The cycle is the unit of the retry because the two
// statements are not re-sendable in the same way: see this file's opening.
//
// None of it happens under the composition lock. Every statement here waits for the
// transactions already in the database, which is what the file's own autocommit
// statement is released to do (kit/db/migrate.go), and the lock is the one every other
// boot queues behind: a run that holds it while it waits for a transaction that is
// waiting for it is the cycle, and the pick lands on the wait that started first, which
// is this one. Measured with the lock held across the repair, `make check` picked this
// session out of that cycle on every cycle of one file — five picks, the whole of
// repairAttempts, and a refused file at the end of it.
//
// An answer that is not the detector's pick comes back as it arrived: a wait that ran
// out stays the refusal that send makes it, as it was before this function existed, and
// a statement that is simply wrong says so on the first answer it gets. What the cycles
// cannot settle refuses the file, which records nothing and leaves it pending for the
// next boot with the object as it found it.
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
