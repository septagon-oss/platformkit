// Package db owns database access. Every query runs inside a transaction whose
// scope is a type parameter: a repository that accepts Tx[Tenant] cannot be
// handed a Tx[System], and nothing can run outside a transaction. The tenant is
// applied with set_config(..., true) so Postgres row-level security enforces
// isolation; there is no Go-side tenant predicate anywhere.
//
// This package is the only place in the program that writes a platformkit.*
// setting. scripts/check_gucs.sh keeps it that way, because those settings are
// USERSET: any statement could rewrite them, so the barrier is a grep and a
// re-read (see Run and RunSystem), not a database privilege.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Pool bounds application connections. A pool is per process, so deployments
// must account for every replica and leave PostgreSQL capacity for maintenance.
type Pool struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DefaultPool preserves the existing 16 open, four idle, 30 minute policy.
func DefaultPool() Pool { return Pool{16, 4, 30 * time.Minute} }

// Validate refuses unbounded connections and inconsistent limits before IO.
// Zero idle connections disables reuse; zero lifetime disables retirement.
func (p Pool) Validate() error {
	switch {
	case p.MaxOpenConns < 1:
		return fmt.Errorf("database.max_open_conns must be positive")
	case p.MaxIdleConns < 0 || p.MaxIdleConns > p.MaxOpenConns:
		return fmt.Errorf("database.max_idle_conns must be between zero and max_open_conns")
	case p.ConnMaxLifetime < 0:
		return fmt.Errorf("database.conn_max_lifetime cannot be negative")
	}
	return nil
}

// Conn is the application connection (role platformkit_app, NOSUPERUSER).
type Conn struct {
	db   *gorm.DB
	pool *sql.DB
}

// Open connects as the application role. It refuses a role that row-level
// security would not bind, because such a connection would make every
// isolation test and every policy in migrations/ decorative.
func Open(ctx context.Context, url string) (*Conn, error) {
	return OpenWithPool(ctx, url, DefaultPool())
}

// OpenWithPool applies explicit limits while retaining Open's role checks.
func OpenWithPool(ctx context.Context, url string, pool Pool) (*Conn, error) {
	if err := pool.Validate(); err != nil {
		return nil, err
	}

	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		// GORM is the SQL executor and nothing else: no callbacks, no
		// plugins, no implicit transaction around a write.
		Logger:                 logger.Discard,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	c := &Conn{db: gdb, pool: sqlDB}
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)

	var (
		role         string
		unrestricted bool
	)
	const q = `SELECT rolname, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`
	if err := sqlDB.QueryRowContext(ctx, q).Scan(&role, &unrestricted); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("db: open: read current role: %w", err)
	}
	if unrestricted {
		_ = c.Close()
		return nil, fmt.Errorf("db: open: role %q is SUPERUSER or has BYPASSRLS, so row-level security would not apply; connect as an unprivileged role", role)
	}
	return c, nil
}

// Stats returns a snapshot of pool occupancy and cumulative connection waits.
// It exposes no connection or query capability.
func (c *Conn) Stats() sql.DBStats { return c.pool.Stats() }

// Close releases the pool.
func (c *Conn) Close() error { return c.pool.Close() }
