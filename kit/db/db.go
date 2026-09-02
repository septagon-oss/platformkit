// Package db owns database access. Every query runs inside a transaction whose
// scope is a type parameter: a repository that accepts Tx[Tenant] cannot be
// handed a Tx[System], and nothing can run outside a transaction. The tenant is
// applied with set_config(..., true) so Postgres row-level security enforces
// isolation; there is no Go-side tenant predicate anywhere.
package db

import (
	"context"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Conn is the application connection (role platformkit_app, NOSUPERUSER).
type Conn struct {
	db *gorm.DB

	// owner marks a connection opened as the role that owns the schema:
	// migrations and TestSchema, which need DDL rights. An owner connection
	// skips the superuser check and is refused by Run and RunSystem, so no
	// application code can reach the database with row-level security off.
	owner bool
}

// Open connects as the application role. It refuses a role that row-level
// security would not bind, because such a connection would make every
// isolation test and every policy in migrations/ decorative.
func Open(ctx context.Context, url string) (*Conn, error) {
	c, err := open(url, false)
	if err != nil {
		return nil, err
	}
	role, unrestricted, err := c.role(ctx)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	if unrestricted {
		_ = c.Close()
		return nil, fmt.Errorf("db: open: role %q is SUPERUSER or has BYPASSRLS, so row-level security would not apply; connect as an unprivileged role", role)
	}
	return c, nil
}

// openOwner connects as the role that owns the schema. It is unexported so the
// only callers are Migrate and TestSchema.
func openOwner(url string) (*Conn, error) { return open(url, true) }

func open(url string, owner bool) (*Conn, error) {
	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		// GORM is the SQL executor and nothing else: no callbacks, no
		// plugins, no implicit transaction around a write.
		Logger:                 logger.Discard,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	return &Conn{db: gdb, owner: owner}, nil
}

// role reports the connected role and whether it escapes row-level security.
func (c *Conn) role(ctx context.Context) (string, bool, error) {
	sqlDB, err := c.db.DB()
	if err != nil {
		return "", false, fmt.Errorf("db: open: %w", err)
	}
	var (
		name         string
		unrestricted bool
	)
	const q = `SELECT rolname, rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`
	if err := sqlDB.QueryRowContext(ctx, q).Scan(&name, &unrestricted); err != nil {
		return "", false, fmt.Errorf("db: open: read current role: %w", err)
	}
	return name, unrestricted, nil
}

// Close releases the pool.
func (c *Conn) Close() error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("db: close: %w", err)
	}
	return sqlDB.Close()
}

// Exec runs one statement outside any transaction, as the owner role. It is the
// DDL door for migrations and tests; an application connection is refused,
// because application queries belong in Run or RunSystem where a tenant applies.
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) error {
	if !c.owner {
		return fmt.Errorf("db: Exec needs the owner connection; run application queries through Run or RunSystem")
	}
	return c.exec(ctx, sql, args...)
}

func (c *Conn) exec(ctx context.Context, query string, args ...any) error {
	sqlDB, err := c.db.DB()
	if err != nil {
		return fmt.Errorf("db: exec: %w", err)
	}
	if _, err := sqlDB.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("db: exec %q: %w", query, err)
	}
	return nil
}
