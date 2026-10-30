package db

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
)

// MigrationInspection describes the selected migration sources against one
// read-only snapshot of the migration ledger.
type MigrationInspection struct {
	// Pending names selected migration files that have no matching ledger row.
	Pending []string
	// Problem names immutable history that does not match the selected release.
	Problem string
}

type appliedMigration struct {
	owner, name, checksum string
	version               int64
}

// InspectMigrations compares selected files with their applied history without
// creating the ledger, taking a migration lock, or changing database state.
// The URL is the migration connection URL; sources are the same ordered sources
// passed to MigrateWithOptions. Connection, permission, and ledger read failures
// are returned without driver text, which may contain connection credentials.
func InspectMigrations(ctx context.Context, url string, sources ...MigrationSource) (MigrationInspection, error) {
	migrations, adoptions, err := readMigrations(sources)
	if err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: selected migration files are invalid")
	}
	pool, err := sql.Open("pgx", url)
	if err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: database connection is unavailable")
	}
	defer pool.Close()
	tx, err := pool.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: database connection is unavailable")
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: migration history cannot be read")
	}
	if !exists {
		return compareMigrationHistory(migrations, adoptions, nil), nil
	}
	return inspectLedger(ctx, tx, migrations, adoptions)
}

func inspectLedger(ctx context.Context, tx *sql.Tx, migrations []migration, adoptions []adoption) (MigrationInspection, error) {
	rows, err := tx.QueryContext(ctx, `SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version`)
	if err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: migration history cannot be read")
	}
	defer rows.Close()
	var applied []appliedMigration
	for rows.Next() {
		var item appliedMigration
		if err := rows.Scan(&item.owner, &item.version, &item.name, &item.checksum); err != nil {
			return MigrationInspection{}, fmt.Errorf("db: inspect migrations: migration history cannot be read")
		}
		applied = append(applied, item)
	}
	if err := rows.Err(); err != nil {
		return MigrationInspection{}, fmt.Errorf("db: inspect migrations: migration history cannot be read")
	}
	return compareMigrationHistory(migrations, adoptions, applied), nil
}

func compareMigrationHistory(migrations []migration, adoptions []adoption, applied []appliedMigration) MigrationInspection {
	selected := make(map[migrationID]migration, len(migrations))
	owners := make(map[string]bool)
	adopted := make(map[migrationID]adoption, len(adoptions))
	for _, item := range migrations {
		selected[item.migrationID] = item
		owners[item.owner] = true
	}
	for _, item := range adoptions {
		adopted[migrationID{owner: item.from, version: item.version}] = item
	}
	seen := make(map[migrationID]bool, len(migrations))
	latest := make(map[string]int64, len(owners))
	for _, row := range applied {
		id := migrationID{owner: row.owner, version: row.version}
		if item, ok := adopted[id]; ok {
			if item.name != row.name || item.checksum != row.checksum {
				return MigrationInspection{Problem: "applied migration history differs from the selected release"}
			}
			seen[item.migrationID] = true
			latest[item.owner] = max(latest[item.owner], item.version)
			continue
		}
		if !owners[row.owner] {
			continue
		}
		file, ok := selected[id]
		if !ok || file.name != row.name || file.checksum != row.checksum {
			return MigrationInspection{Problem: "applied migration history differs from the selected release"}
		}
		seen[id] = true
		latest[row.owner] = max(latest[row.owner], row.version)
	}
	var pending []string
	for _, item := range migrations {
		if !seen[item.migrationID] {
			if item.version < latest[item.owner] {
				return MigrationInspection{Problem: "pending migration precedes applied history"}
			}
			pending = append(pending, item.owner+"/"+item.name)
		}
	}
	slices.Sort(pending)
	return MigrationInspection{Pending: pending}
}
