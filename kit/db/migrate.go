package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrationSource is the SQL owned by one capability. Owner is a stable name;
// versions increase within that owner, independently of every other capability.
// Files contains <version>_<name>.up.sql files at its root.
type MigrationSource struct {
	Owner string
	Files fs.FS
}

// Migrate validates the selected histories, then applies pending SQL in source
// order and numeric version order. Each file and its history row commit in one
// transaction. Files must contain transactional PostgreSQL SQL; transaction
// control and operations such as CREATE INDEX CONCURRENTLY belong outside them.
// Removing a source from the composition leaves its tables and history intact.
func Migrate(ctx context.Context, migrateURL string, sources ...MigrationSource) error {
	migrations, err := readMigrations(sources)
	if err != nil {
		return err
	}
	pool, err := sql.Open("pgx", migrateURL)
	if err != nil {
		return fmt.Errorf("db: migrate: open: %w", err)
	}
	defer pool.Close()
	conn, err := pool.Conn(ctx)
	if err != nil {
		return fmt.Errorf("db: migrate: connect: %w", err)
	}
	defer conn.Close()

	// A session lock covers the whole composition, including history checks.
	// Replicas use the same pinned connection for locking and unlocking.
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", 7240101); err != nil {
		return fmt.Errorf("db: migrate: lock: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(cleanup, "SELECT pg_advisory_unlock($1)", 7240101)
	}()

	if _, err := conn.ExecContext(ctx, migrationLedger); err != nil {
		return fmt.Errorf("db: migrate: prepare history: %w", err)
	}
	pending, err := pendingMigrations(ctx, conn, migrations)
	if err != nil {
		return err
	}
	for _, migration := range pending {
		if err := applyMigration(ctx, conn, migration); err != nil {
			return fmt.Errorf("db: migrate: %s/%s: %w", migration.owner, migration.name, err)
		}
	}
	return nil
}

// Default table grants serve business data. Migration history belongs only to
// its owner: an application role must not be able to forge or erase progress.
// PostgreSQL executes this statement batch in one implicit transaction.
const migrationLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	owner text NOT NULL,
	version bigint NOT NULL CHECK (version > 0),
	name text NOT NULL,
	checksum text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (owner, version)
);
DO $$
DECLARE recipient text;
BEGIN
	FOR recipient IN
		SELECT DISTINCT CASE WHEN acl.grantee = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(acl.grantee)) END
		FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) acl
		WHERE c.oid = 'schema_migrations'::regclass AND acl.grantee <> c.relowner
	LOOP
		EXECUTE 'REVOKE ALL ON TABLE schema_migrations FROM ' || recipient || ' CASCADE';
	END LOOP;
END $$;`

func pendingMigrations(ctx context.Context, conn *sql.Conn, migrations []migration) ([]migration, error) {
	remaining := make(map[migrationID]migration, len(migrations))
	latest := map[string]int64{}
	for _, migration := range migrations {
		remaining[migration.migrationID] = migration
		latest[migration.owner] = 0
	}
	rows, err := conn.QueryContext(ctx, "SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version")
	if err != nil {
		return nil, fmt.Errorf("db: migrate: read history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id migrationID
		var name, checksum string
		if err := rows.Scan(&id.owner, &id.version, &name, &checksum); err != nil {
			return nil, fmt.Errorf("db: migrate: read history: %w", err)
		}
		if _, selected := latest[id.owner]; !selected {
			continue
		}
		migration, found := remaining[id]
		if !found {
			return nil, fmt.Errorf("db: migrate: %s/%s was applied but is missing from this release", id.owner, name)
		}
		if migration.name != name || migration.checksum != checksum {
			return nil, fmt.Errorf("db: migrate: %s/%s changed after it was applied; add a new migration", id.owner, name)
		}
		latest[id.owner] = max(latest[id.owner], id.version)
		delete(remaining, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: migrate: read history: %w", err)
	}
	var pending []migration
	for _, migration := range migrations {
		if _, needed := remaining[migration.migrationID]; !needed {
			continue
		}
		if migration.version < latest[migration.owner] {
			return nil, fmt.Errorf("db: migrate: %s/%s precedes applied version %d; append a new version", migration.owner, migration.name, latest[migration.owner])
		}
		pending = append(pending, migration)
	}
	return pending, nil
}

func applyMigration(ctx context.Context, conn *sql.Conn, migration migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, migration.sql); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO schema_migrations (owner, version, name, checksum) VALUES ($1, $2, $3, $4)",
		migration.owner, migration.version, migration.name, migration.checksum); err != nil {
		return err
	}
	return tx.Commit()
}
