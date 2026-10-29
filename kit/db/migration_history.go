package db

import (
	"context"
	"database/sql"
	"fmt"
)

// Default table grants serve business data. Migration history belongs only to
// its owner: an application role must not be able to forge or erase progress.
// PostgreSQL executes this statement batch in one implicit transaction.
const createLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
	owner text NOT NULL,
	version bigint NOT NULL CHECK (version > 0),
	name text NOT NULL,
	checksum text NOT NULL,
	applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (owner, version)
)`

// createProgress is where a backfill that has not finished says where to start. It is
// beside the ledger and not inside it because every reader of the ledger assumes a row
// there means applied forever, and an unfinished drain must not look like one. The row
// is deleted in the transaction that writes the history row, so the two tables
// together hold one fact. The cursor is nullable because it holds a key rendered as text,
// and "no window has committed yet" is not a key: for a text-keyed table the empty string
// is a legal key and the smallest one there is, so a NOT NULL column defaulting to the empty
// string would say both things with one value, and a drain over such a table would never move
// past the row it started on (see drainPos).
const createProgress = `
CREATE TABLE IF NOT EXISTS schema_migration_backfill (
	owner text NOT NULL,
	version bigint NOT NULL CHECK (version > 0),
	cursor text,
	updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	PRIMARY KEY (owner, version)
)`

// revokeFromApplicationRole is the sweep that keeps both tables the runner owns
// unwritable: a ledger an application can edit is a release that never happened, and
// a progress row it can edit is a backfill that repeats rows.
const revokeFromApplicationRole = `
DO $$
DECLARE recipient text;
BEGIN
	FOR recipient IN
		SELECT DISTINCT CASE WHEN acl.grantee = 0 THEN 'PUBLIC'
			ELSE quote_ident(pg_get_userbyid(acl.grantee)) END
		FROM pg_class c CROSS JOIN LATERAL aclexplode(c.relacl) acl
		WHERE c.oid = %s::regclass AND acl.grantee <> c.relowner
	LOOP
		EXECUTE 'REVOKE ALL ON TABLE %s FROM ' || recipient || ' CASCADE';
	END LOOP;
END $$`

// createOwnTables makes the two tables the runner owns, if they are not there.
// CREATE TABLE IF NOT EXISTS over a table that exists is a catalog lookup, which is
// why a drain may run this half and not the sweep below.
func createOwnTables(ctx context.Context, conn *sql.Conn) error {
	for _, statement := range []string{createLedger, createProgress} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("db: migrate: create history: %w", err)
		}
	}
	return nil
}

// revokeOwnTables takes away what the application must not hold. It runs behind the
// composition's advisory lock so that one run sweeps once, and because the sweep and
// the CREATE beside it are the same statement about who owns the two tables — not
// because the REVOKE has to wait for anything: measured, it takes SHARE UPDATE
// EXCLUSIVE, which conflicts with another REVOKE or ALTER and not with the reads and
// writes of a running request or a committing batch (a plain ALTER TABLE on the same
// table behind the same reader waits; this statement completes in twenty
// milliseconds).
func revokeOwnTables(ctx context.Context, conn *sql.Conn) error {
	for _, statement := range []string{
		fmt.Sprintf(revokeFromApplicationRole, "'schema_migrations'", "schema_migrations"),
		fmt.Sprintf(revokeFromApplicationRole, "'schema_migration_backfill'", "schema_migration_backfill"),
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("db: migrate: revoke history: %w", err)
		}
	}
	return nil
}

// adoptHistory re-owns the ledger rows the adoptions name, all in one
// transaction, so a failure part-way leaves the history as it was. A row the
// old owner never applied is not there to adopt, which is every fresh
// installation and every installation that already adopted it; a row that is
// there with another checksum is a file that changed after it was applied.
func adoptHistory(ctx context.Context, conn *sql.Conn, adoptions []adoption) error {
	if len(adoptions) == 0 {
		return nil
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: migrate: adopt history: %w", err)
	}
	defer tx.Rollback()
	for _, a := range adoptions {
		result, err := tx.ExecContext(ctx,
			"UPDATE schema_migrations SET owner = $1 WHERE owner = $2 AND version = $3 AND checksum = $4",
			a.owner, a.from, a.version, a.checksum)
		if err != nil {
			return fmt.Errorf("db: migrate: adopt %s/%s from %s: %w", a.owner, a.name, a.from, err)
		}
		if moved, _ := result.RowsAffected(); moved == 1 {
			continue
		}
		var applied string
		err = tx.QueryRowContext(ctx, "SELECT name FROM schema_migrations WHERE owner = $1 AND version = $2", a.from, a.version).Scan(&applied)
		switch {
		case err == sql.ErrNoRows:
			// Nothing to adopt: never applied under the old owner, or adopted already.
		case err != nil:
			return fmt.Errorf("db: migrate: adopt %s/%s from %s: %w", a.owner, a.name, a.from, err)
		default:
			return fmt.Errorf("db: migrate: %s/%s was applied as %s/%s with different content; adoption keeps the bytes that ran, add a new migration for the change",
				a.owner, a.name, a.from, applied)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: migrate: adopt history: %w", err)
	}
	return nil
}

// migrationHistory is what the ledger said before this run began, which is the
// only thing a release rule can be judged against: the contract half asks whether
// its expansion has *already* applied, and a drain asks whether anybody was
// reading this owner before this run started.
type migrationHistory struct {
	applied map[migrationID]bool
	latest  map[string]int64
}

func pendingMigrations(ctx context.Context, conn *sql.Conn, migrations []migration) ([]migration, migrationHistory, error) {
	remaining := make(map[migrationID]migration, len(migrations))
	history := migrationHistory{applied: map[migrationID]bool{}, latest: map[string]int64{}}
	for _, migration := range migrations {
		remaining[migration.migrationID] = migration
		history.latest[migration.owner] = 0
	}
	rows, err := conn.QueryContext(ctx, "SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version")
	if err != nil {
		return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id migrationID
		var name, checksum string
		if err := rows.Scan(&id.owner, &id.version, &name, &checksum); err != nil {
			return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
		}
		if _, selected := history.latest[id.owner]; !selected {
			continue
		}
		migration, found := remaining[id]
		if !found {
			return nil, history, fmt.Errorf("db: migrate: %s/%s was applied but is missing from this release", id.owner, name)
		}
		if migration.name != name || migration.checksum != checksum {
			return nil, history, fmt.Errorf("db: migrate: %s/%s changed after it was applied; add a new migration", id.owner, name)
		}
		history.applied[id] = true
		history.latest[id.owner] = max(history.latest[id.owner], id.version)
		delete(remaining, id)
	}
	if err := rows.Err(); err != nil {
		return nil, history, fmt.Errorf("db: migrate: read history: %w", err)
	}
	var pending []migration
	for _, migration := range migrations {
		if _, needed := remaining[migration.migrationID]; !needed {
			continue
		}
		if migration.version < history.latest[migration.owner] {
			return nil, history, fmt.Errorf("db: migrate: %s/%s precedes applied version %d; append a new version", migration.owner, migration.name, history.latest[migration.owner])
		}
		pending = append(pending, migration)
	}
	return pending, history, nil
}

const insertHistory = "INSERT INTO schema_migrations (owner, version, name, checksum) VALUES ($1, $2, $3, $4)"

// recordHistory commits one file's history row in a transaction of its own, which
// is what an autocommit file leaves behind: its statement already ran, and the row
// that says so is the one thing still allowed to be atomic.
func recordHistory(ctx context.Context, conn *sql.Conn, migration migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := recordIn(ctx, tx, migration); err != nil {
		return err
	}
	return tx.Commit()
}

// recordRerunnableHistory is recordHistory for the one file whose statement another
// session may have been running at the same moment: a conflict on the primary key is
// not this run failing, it is the other run having succeeded.
func recordRerunnableHistory(ctx context.Context, conn *sql.Conn, migration migration) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, insertHistory+` ON CONFLICT (owner, version) DO NOTHING`,
		migration.owner, migration.version, migration.name, migration.checksum); err != nil {
		return err
	}
	return tx.Commit()
}

func recordIn(ctx context.Context, tx *sql.Tx, migration migration) error {
	_, err := tx.ExecContext(ctx, insertHistory, migration.owner, migration.version, migration.name, migration.checksum)
	return err
}
