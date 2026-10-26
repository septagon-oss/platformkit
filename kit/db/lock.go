package db

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Unlock releases an advisory lock taken by TryLockErr and the connection that
// holds it. It runs whatever has happened to the caller's context since the lock
// was taken — a job whose request ended must still put the lock down — bounded to
// five seconds of its own, and a release it cannot confirm ends the session, which
// drops the key with it. It is safe to call more than once and from more than one
// goroutine: the release happens once and a second call does nothing. A nil Unlock
// is never returned with a nil error.
type Unlock func()

// ErrHeld says another process already holds the lock. It is the ordinary outcome
// on every replica but one and not a failure of the lock or of the work: nothing
// this call did changed anything in the database, and the caller may ask again.
// Like ErrContended it is a report about contention and not a `refusal <id>`
// about this database, so an operator reads it as a tick that did not need to run.
var ErrHeld = errors.New("db: lock is held: another process holds it; nothing this call changed, and it may be asked again")

// TryLockErr takes the Postgres advisory lock named name, so that at most one
// process in a cluster runs the work behind it, and answers the one question a
// caller has: ErrHeld when somebody else holds it, and the release otherwise.
//
// The key is Postgres's own hash of the name, so two processes that spell it the
// same way take the same lock without a registry of numbers to keep in step.
// Locks are per-database and independent of any schema.
//
// The lock is session-level and lives on a connection of its own, pinned until the
// returned Unlock runs. It has to be: the work it guards is not a transaction — a
// periodic job opens transactions of its own, and a lock held by an enclosing
// transaction would make every db.Run inside it a scope mismatch. A connection or
// query failure answers an error wrapping the same "db: lock %q" sentence with a
// nil Unlock and the lock not held; c must outlive the Unlock it returns.
func TryLockErr(ctx context.Context, c *Conn, name string) (Unlock, error) {
	sqlDB, err := c.db.DB()
	if err != nil {
		return nil, fmt.Errorf("db: lock %q: %w", name, err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("db: lock %q: %w", name, err)
	}
	var taken bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtext($1)::bigint)", name).Scan(&taken); err != nil {
		// The server may have acquired the session lock before the response failed.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		_ = conn.Close()
		return nil, fmt.Errorf("db: lock %q: %w", name, err)
	}
	if !taken {
		_ = conn.Close()
		return nil, ErrHeld
	}
	// The release happens once however often the returned function is called, so a
	// caller that hands the same Unlock to two deferred paths cannot end the session
	// twice or report a release nobody asked for.
	var once sync.Once
	return func() {
		once.Do(func() {
			// The unlock runs on the connection that took the lock, and runs even
			// when the job's context is already cancelled. Bound that independent
			// cleanup, and discard a session whose release cannot be confirmed.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			var released bool
			if err := conn.QueryRowContext(cleanup, "SELECT pg_advisory_unlock(hashtext($1)::bigint)", name).Scan(&released); err != nil || !released {
				_ = conn.Raw(func(any) error { return driver.ErrBadConn })
			}
			_ = conn.Close()
		})
	}, nil
}

// TryLock takes a Postgres advisory lock named name, so that at most one
// process in a cluster runs the work behind it. It reports false when another
// process already holds it, which is the ordinary outcome on every replica but
// one and not an error.
//
// The lock is session-level and lives on a connection of its own, pinned until
// unlock is called. It has to be: the work it guards is not a transaction — a
// periodic job opens transactions of its own, and a lock held by an enclosing
// transaction would make every db.Run inside it a scope mismatch.
//
// Deprecated: Use TryLockErr, whose ErrHeld names the held case a false in a
// triple whose third slot is a failure cannot.
func TryLock(ctx context.Context, c *Conn, name string) (unlock func(), ok bool, err error) {
	unlock, err = TryLockErr(ctx, c, name)
	switch {
	case errors.Is(err, ErrHeld):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}
	return unlock, true, nil
}
