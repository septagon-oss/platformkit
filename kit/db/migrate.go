package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrationSource is the SQL owned by one capability. Owner is a stable name;
// versions increase within that owner, independently of every other capability.
// Files contains <version>_<name>.up.sql files at its root.
type MigrationSource struct {
	Owner string
	Files fs.FS
	// Adopts is history this owner takes over from another: ledger rows the
	// named owner applied whose version and checksum match a file of this
	// source, under the same version number. Migrate re-owns them in one
	// transaction before it reads any history, so the adopted files are
	// neither missing from the new owner nor pending for the new one, and
	// their SQL never runs again. It is how a module takes the files the
	// foundation once applied under its own name (docs/adr/0011); a fresh
	// installation has nothing to adopt and the declaration is a no-op.
	Adopts []Adoption
	// RulesFrom is the first version checked by migration_rules.go; zero checks all.
	// It exempts immutable applied history. A floor past the source's highest version
	// plus one exempts nothing: pending files are checked after ledger reading.
	// The source owner supplies this historical boundary.
	RulesFrom int64
}

// Adoption names one previous owner and the versions of its ledger rows that
// are now the adopting source's files. Every version must be a file of the
// adopting source; an adopted row whose checksum differs from that file is a
// changed migration and refuses, like any other changed applied file.
type Adoption struct {
	Owner    string
	Versions []int64
}

// Sub is fs.Sub for an embedded migrations directory: the files under dir as a
// source's root, which is where readMigrations looks. It panics where fs.Sub
// would return an error, because dir is a literal written beside a //go:embed
// directive and a wrong one is a mistake with no runtime to report to.
func Sub(fsys fs.FS, dir string) fs.FS {
	files, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("db: Sub: " + err.Error())
	}
	return files
}

// The runner's budgets are its own patience and never the operator's: a migration
// that queues behind the running application must stop and say so within seconds,
// which is the difference between a release that waits and an installation that
// appears to have stopped. defaultLockTimeout is the wait a migration is given for
// a lock — five seconds of waiting, never of holding. It is spelled as PostgreSQL
// spells it because current_setting answers in the text a value was set with, and
// an operator reading the setting back should read what the documentation says.
const defaultLockTimeout = 5 * time.Second

// MigrationBudget is how long one migration file may wait — for a lock, and for a
// statement overall — before the runner stops it, put on the runner's own session
// before every file and every batch. A nil field is the documented default.
//
// The statement budget defaults to no bound deliberately: the wait no installation
// should sit through is the lock wait, and a legitimate index build on a large
// table is the statement a duration bound would kill.
type MigrationBudget struct {
	LockTimeout      *time.Duration
	StatementTimeout *time.Duration
}

// DefaultMigrationBudget is what a run that names no budget uses: five seconds for
// a lock, and no bound on a statement.
func DefaultMigrationBudget() MigrationBudget {
	lock := defaultLockTimeout
	return MigrationBudget{LockTimeout: &lock}
}

// budgets returns the two durations in force, defaults filled in. A zero statement
// duration is PostgreSQL's own "no limit".
func (b MigrationBudget) budgets() (lock, statement time.Duration) {
	lock, statement = defaultLockTimeout, 0
	if b.LockTimeout != nil {
		lock = *b.LockTimeout
	}
	if b.StatementTimeout != nil {
		statement = *b.StatementTimeout
	}
	return lock, statement
}

// statement is the pair of settings. On the session and not SET LOCAL: measured, a
// SET LOCAL outside a transaction block is a warning and a no-op, and the autocommit
// mode has no block for a setting to be local to. The connection closes when the run
// ends, which is the reset.
func (b MigrationBudget) statement() string {
	lock, statement := b.budgets()
	return fmt.Sprintf("SET lock_timeout TO '%s'; SET statement_timeout TO '%s'",
		pgDuration(lock, "5s"), pgDuration(statement, "0"))
}

// validate refuses a budget that could not be a wait. The caller names the door it
// was refused at, because both Migrate and Backfill take one.
func (b MigrationBudget) validate() error {
	if b.LockTimeout != nil && *b.LockTimeout < 0 {
		return fmt.Errorf("lock budget %s is a wait in the past", *b.LockTimeout)
	}
	if b.StatementTimeout != nil && *b.StatementTimeout < 0 {
		return fmt.Errorf("statement budget %s is a wait in the past", *b.StatementTimeout)
	}
	return nil
}

// pgDuration renders a duration the way a PostgreSQL time setting takes it, and the
// documented default in the words the documentation uses.
func pgDuration(d time.Duration, written string) string {
	if d == 0 {
		return written
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	return fmt.Sprintf("%dms", d.Milliseconds())
}

// ErrContended is what a migration that could not take a lock within the runner's
// budget returns: not a failure of the migration. Nothing new was applied, the files
// already applied stay applied, and the operator may run it again. The runner does
// not wait and retry inside itself, because it holds the composition's advisory lock
// while it waits, and a queue inside that lock stops every other replica's boot.
var ErrContended = errors.New("db: migration is contended: it could not take a lock within its budget; nothing this run had not already applied was applied, and it may be run again")

// Migrate validates selected histories and applies pending files in source and
// version order, using each header's transactional, autocommit or backfill mode.
// It uses default budgets. Invalid files fail before connecting; owner planning
// fails before that owner's pending files run. Worker-owned drains defer later
// files and return nil. Earlier committed files and batches survive later errors.
// See README.md for execution modes and backfill ownership.
//
// Deprecated: Use MigrateWithOptions with the zero MigrateOptions, which runs on
// these same default budgets and declares nothing.
func Migrate(ctx context.Context, migrateURL string, sources ...MigrationSource) error {
	return MigrateWith(ctx, migrateURL, MigrationBudget{}, sources...)
}

// MigrateWith is Migrate with the budgets a deployment named in its configuration.
// Migrate is this function with the documented defaults, which is why every caller
// that does not name a budget still gets the lock budget.
//
// Deprecated: Use MigrateWithOptions with MigrateOptions{Budget: budget}, which is
// this call and nothing more.
func MigrateWith(ctx context.Context, migrateURL string, budget MigrationBudget, sources ...MigrationSource) error {
	return MigrateDeclaring(ctx, migrateURL, budget, Declaration{}, sources...)
}

// Declaration is what the composition running a migration says about itself, so
// that a file which has to place a row belonging to somebody can prove whose it
// is instead of guessing. It is the one door a migration has for a fact the
// database does not hold: a schema half runs before any request, so the boot is
// the only party that knows which app it is and which hosts it serves.
//
// The three settings arrive as session state on the migration's own connection,
// which is opened for this run and closed when it ends. They are placeholder
// GUCs, the same mechanism 000001 uses for platformkit.tenant_id, and a file
// reads them with current_setting(name, true) — where the answer is NULL when the
// boot said nothing, which is a different fact from saying something empty. That
// distinction is the whole of App's pointer: a composition that names no slug is
// the deployment of one app and declares it as such, and a composition that
// declares nothing may only be migrating a database with nothing to place.
type Declaration struct {
	// App is the composition's own slug, or nil when its configuration names
	// none. An empty string is the single-app deployment named on purpose; nil
	// is a boot that said nothing.
	App *string
	// Hosts are the hosts this composition serves its tenants at. A back-fill
	// places an existing tenant under App only when every host the tenant holds
	// is one of these — which is what "proves its input" means when the input is
	// a set of tenants the database already has.
	Hosts []string
	// Tenants is the operator's explicit mapping, tenant slug to app slug, for
	// the tenant the hosts cannot place. A file that reads it places every row it
	// names and refuses the rows it does not — the refusal reaches a boot that
	// named itself, and migrations/000060_tenant_app_place.up.sql says why.
	Tenants map[string]string
}

// declare puts the declaration on the session. Values travel as parameters, so a
// slug or host that holds a quote is a value and never a statement.
func (d Declaration) declare(ctx context.Context, conn *sql.Conn) error {
	// Every one of these scans its row into a value it throws away. A Row that is
	// never read is never closed, the connection keeps the delivery open, and the
	// deferred conn.Close() at the end of the run waits for it: measured, a
	// declaration that used Row.Err() hung the migration's own teardown and the
	// session died holding the composition's advisory lock, so the next run in the
	// same database never came back.
	var declared string
	if d.App != nil {
		if err := conn.QueryRowContext(ctx, `SELECT set_config('platformkit.app', $1, false)`, *d.App).Scan(&declared); err != nil {
			return fmt.Errorf("declaring the app: %w", err)
		}
	}
	if len(d.Hosts) > 0 {
		if err := conn.QueryRowContext(ctx, `SELECT set_config('platformkit.app_hosts', $1, false)`, strings.Join(d.Hosts, ",")).Scan(&declared); err != nil {
			return fmt.Errorf("declaring the hosts: %w", err)
		}
	}
	if len(d.Tenants) > 0 {
		pairs := make([]string, 0, len(d.Tenants))
		for tenant, app := range d.Tenants {
			pairs = append(pairs, tenant+"="+app)
		}
		slices.Sort(pairs)
		if err := conn.QueryRowContext(ctx, `SELECT set_config('platformkit.app_tenants', $1, false)`, strings.Join(pairs, ",")).Scan(&declared); err != nil {
			return fmt.Errorf("declaring the tenant mapping: %w", err)
		}
	}
	return nil
}

// MigrateDeclaring is MigrateWith plus what the boot declares about itself. An
// empty Declaration declares nothing, which is what every caller that predates
// the tenant app column already meant.
//
// Deprecated: Use MigrateWithOptions, which takes the budget and the declaration
// as the one MigrateOptions it runs on.
func MigrateDeclaring(ctx context.Context, migrateURL string, budget MigrationBudget, decl Declaration, sources ...MigrationSource) error {
	return MigrateWithOptions(ctx, migrateURL, MigrateOptions{Budget: budget, Declaration: decl}, sources...)
}

// compositionLockKey is the first half of the advisory lock one migration run holds for
// its whole composition, so that two replicas do not apply the same file at once. The
// second half is the namespace the run resolves to: see holdCompositionLock, which says
// why the key is one namespace's and not one database's.
const compositionLockKey = 7240101

// stepAwayFromTheCompositionLock releases the composition lock and its lock
// timeout for an autocommit statement. The statement timeout remains in
// force while concurrent transactions finish.
func (r *runner) stepAwayFromTheCompositionLock(ctx context.Context) error {
	if err := r.setBudgets(ctx, noLockBudget); err != nil {
		return err
	}
	return r.releaseCompositionLock(ctx)
}
