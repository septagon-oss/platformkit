package db

import (
	"context"
	"fmt"
)

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
// The key is Postgres's own hash of the name, so two processes that spell the
// name the same way take the same lock without a registry of numbers to keep in
// step. Locks are per-database and independent of any schema.
func TryLock(ctx context.Context, c *Conn, name string) (unlock func(), ok bool, err error) {
	sqlDB, err := c.db.DB()
	if err != nil {
		return nil, false, fmt.Errorf("db: lock %q: %w", name, err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("db: lock %q: %w", name, err)
	}
	var taken bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtext($1)::bigint)", name).Scan(&taken); err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("db: lock %q: %w", name, err)
	}
	if !taken {
		_ = conn.Close()
		return nil, false, nil
	}
	return func() {
		// The unlock runs on the connection that took the lock, and runs even
		// when the job's context is already cancelled; returning the connection
		// to the pool while it still holds a lock would poison it.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtext($1)::bigint)", name)
		_ = conn.Close()
	}, true, nil
}
