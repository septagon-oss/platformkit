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
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Pool bounds. One instance holds at most 16 server connections, which a small
// Postgres can multiply by a dozen replicas and still breathe; four idle ones
// absorb a burst without holding a backend open per goroutine; and a 30 minute
// lifetime lets a failover or a rolling database upgrade drain the pool without
// anyone restarting the app. They are constants rather than config keys because
// no deployment has yet had a reason to differ, and a key nothing reads does not
// belong in the configuration surface.
const (
	maxOpenConns    = 16
	maxIdleConns    = 4
	connMaxLifetime = 30 * time.Minute
)

// Conn is the application connection (role platformkit_app, NOSUPERUSER).
type Conn struct{ db *gorm.DB }

// Open connects as the application role. It refuses a role that row-level
// security would not bind, because such a connection would make every
// isolation test and every policy in migrations/ decorative.
func Open(ctx context.Context, url string) (*Conn, error) {
	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		// GORM is the SQL executor and nothing else: no callbacks, no
		// plugins, no implicit transaction around a write.
		Logger:                 logger.Discard,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	c := &Conn{db: gdb}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

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

// Close releases the pool.
func (c *Conn) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("db: close: %w", err)
	}
	return sqlDB.Close()
}
