package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// TestTheRepairCommandPrintsWhatItCommitted reads what repair-roles printed, the
// half of the command no other case looks at: the listing is how the operator
// decides, so a run that changed rows without naming them — or named a row its
// transaction then failed to commit — failed at the one thing it is for.
// TestTheRepairCommandListsBeforeItRemoves reads the rows and says so itself;
// nothing read the printing. Every line here is checked against the row it
// claims: still there after a listing, gone after --remove, a second run has
// nothing left to claim about it, and the sentence that second run prints claims
// only the tenants that run actually read.
//
// The last of those is the one worth reading twice. The walk is
// tenantcontracts.Active — the lister this composition hands every tenant-scoped
// job — so a tenant somebody suspended is never opened by this command, and an
// all-clear phrased about "this installation" reported the rows of a tenant it
// had not read. The grant left in a suspended tenant is inert by definition and
// the hourly sweep is quiet about it for the same reason, so the cure is not to
// reach into the suspended tenant: it is for the all-clear to name its own scope.
// Phrased about the installation, this case fails on the suspended run below,
// which exited 0 and left ghost:read in the row it had just called clean.
func TestTheRepairCommandPrintsWhatItCommitted(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		acme, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}

	// plant and holds are the row an older seeder left, written and read with raw
	// SQL because no route would accept a permission no module defines: the state
	// this command exists for is one a deploy wrote, not one a person is offered.
	plant := func() {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(
				"UPDATE roles SET permissions = array_append(permissions, 'ghost:read') WHERE tenant_id = ? AND name = ?",
				acme.ID, authcontracts.RoleAdmin).Error
		})
		if err != nil {
			t.Fatalf("leave the row an older seeder left: %v", err)
		}
	}

	holds := func(want bool) {
		t.Helper()
		var roles []authcontracts.Role
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Where("tenant_id = ? AND name = ?", acme.ID, authcontracts.RoleAdmin).Find(&roles).Error
		})
		if err != nil {
			t.Fatalf("read the administrator back: %v", err)
		}
		if len(roles) != 1 || slices.Contains(roles[0].Grants, "ghost:read") != want {
			t.Fatalf("administrator holding ghost:read is %v, want it=%v", roles, want)
		}
	}

	// status moves the tenant out of the walk and back: Active is a filter on
	// status, and suspending is how an installation stops a tenant it still holds
	// the rows of.
	status := func(want string) {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec("UPDATE tenants SET status = ? WHERE id = ?", want, acme.ID).Error
		})
		if err != nil {
			t.Fatalf("set the tenant %q: %v", want, err)
		}
	}

	// os.Stdout is the command's only output channel, so the case takes it — to a
	// file rather than a pipe, because the writer is this same process and a pipe
	// would fill and wait, and with the swap undone by a defer, so that a case
	// that fails while holding it does not send its own report to it.
	printed := func(run func() error) []string {
		t.Helper()
		lines, runErr := printedLines(t, run)
		if runErr != nil {
			t.Fatalf("repair-roles: %v", runErr)
		}
		return lines
	}

	plant()
	lines := printed(func() error { return repairRoles([]string{"--config", path}) })
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "acme\tadmin\tseeded grants no composed module defines\t") ||
		!strings.Contains(lines[0], "ghost:read") {
		t.Errorf("the listing printed %q, want one line naming acme's administrator and ghost:read", lines)
	}
	holds(true)

	lines = printed(func() error { return repairRoles([]string{"--config", path, "--remove"}) })
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "acme\tadmin\tremoved\t") ||
		!strings.Contains(lines[0], "ghost:read") {
		t.Errorf("--remove printed %q, want one line saying acme's administrator was removed of ghost:read", lines)
	}
	holds(false)

	if lines := printed(func() error { return repairRoles([]string{"--config", path, "--remove"}) }); len(lines) != 1 ||
		strings.Contains(strings.Join(lines, " "), "ghost:read") ||
		lines[0] != "nothing for this run to take from any active tenant; the hourly sweep reports what is left" {
		t.Errorf("a second run printed %q, want the all-clear that names this run and the tenants it read", lines)
	}

	// The row again, and the tenant out of the walk: the run that reaches neither
	// row has to say what it read rather than that the installation is clean.
	plant()
	status(tenantcontracts.StatusSuspended)
	lines = printed(func() error { return repairRoles([]string{"--config", path, "--remove"}) })
	if len(lines) != 1 || !strings.Contains(lines[0], "active tenant") ||
		strings.Contains(strings.Join(lines, " "), "ghost:read") {
		t.Errorf("a run whose walk reached no tenant printed %q, want an all-clear that names the tenants it read", lines)
	}
	holds(true)

	status(tenantcontracts.StatusActive)
	lines = printed(func() error { return repairRoles([]string{"--config", path}) })
	if len(lines) != 1 || !strings.Contains(lines[0], "ghost:read") {
		t.Errorf("the same grant with the tenant active again printed %q, want it listed", lines)
	}
	holds(true)
}

// TestTheRepairPrintsNothingThatItsCommitRefused is the other half of what the
// case above claims: what the command prints is what committed, not what it meant
// to write.
//
// The refusal is arranged where a real one happens — in the database, after the
// write and before the work is durable — because no composition-level door can
// fail kit/db's commit: db.Run opens the transaction, runs its body once and
// commits it (kit/db/tx.go), and neither Conn nor dbtest offers a way to make
// that commit answer anything other than what the server says. Postgres can. A
// deferrable unique constraint over (tenant_id, permissions) is evaluated at
// COMMIT, so the UPDATE is accepted, the repair has its stale grants in hand and
// a line per role ready to print, and only then is the transaction refused — the
// same shape as the serialisation failure and the contended lock an operator
// actually meets. Printing from inside the transaction, which is where round 8
// found the code had been, prints acme's administrator as removed here.
//
// The row is read back afterwards, because the claim worth making is not only
// that the run kept silent: the removal it could not commit was taken back, and a
// grant the installation still holds is a grant the hourly sweep still reports.
func TestTheRepairPrintsNothingThatItsCommitRefused(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		acme, err = c.tenants.ByHost(ctx, tx, acmeHost)
		return err
	})
	if err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}

	// The row an older seeder left, and then the row that makes committing the
	// repair collide with itself: a second role holding exactly the grant list the
	// repair is about to leave in the administrator. Both are written with raw SQL
	// for the reason the case above gives — no door would offer either of them.
	plant := func() {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(
				"UPDATE roles SET permissions = array_append(permissions, 'ghost:read') WHERE tenant_id = ? AND name = ?",
				acme.ID, authcontracts.RoleAdmin).Error
		})
		if err != nil {
			t.Fatalf("leave the row an older seeder left: %v", err)
		}
	}
	plant()

	collide := func() {
		t.Helper()
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(
				"INSERT INTO roles (tenant_id, name, permissions) SELECT tenant_id, 'shadow', array_remove(permissions, 'ghost:read') FROM roles WHERE tenant_id = ? AND name = ?",
				acme.ID, authcontracts.RoleAdmin).Error
		})
		if err != nil {
			t.Fatalf("plant the row the repaired administrator collides with: %v", err)
		}
		// The constraint itself needs the owner connection: the application role is
		// granted the four words it may use on a table and nothing else, which is the
		// point of row-level security and of this role.
		admin := dbtest.Open(t, cfg.Database.MigrateURL)
		t.Cleanup(func() { _ = admin.Close() })
		if _, err := admin.ExecContext(t.Context(),
			"ALTER TABLE roles ADD CONSTRAINT repair_commit_probe UNIQUE (tenant_id, permissions) DEFERRABLE INITIALLY DEFERRED"); err != nil {
			t.Fatalf("refuse the commit: %v", err)
		}
	}
	collide()

	lines, runErr := printedLines(t, func() error { return repairRoles([]string{"--config", path, "--remove"}) })
	if runErr == nil {
		t.Errorf("the repair reported success although the database refused its transaction at commit: printed %q", lines)
	}
	if len(lines) != 0 {
		t.Errorf("the repair printed %q for a transaction the database refused at commit; a line saying removed is a claim about a row, and no row changed", lines)
	}

	if held := rolesOf(t, c, conn, acme)[authcontracts.RoleAdmin]; !slices.Contains(held, "ghost:read") {
		t.Errorf("the grant the refused commit was about has gone: %v — either the commit went through after all, or the rollback took something else with it", held)
	}
}

// printedLines runs the command with os.Stdout taken to a file and returns the
// lines it printed and the error the run returned. A file rather than a pipe,
// because the writer is this same process and a pipe would fill and wait, and the
// swap is undone before this returns, so that a case which fails while holding it
// does not send its own report to it. A run that printed nothing returns no lines
// and is the answer the case below is looking for.
func printedLines(t *testing.T, run func() error) ([]string, error) {
	t.Helper()
	name := filepath.Join(t.TempDir(), "printed")
	file, err := os.Create(name)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	saved := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = saved }()
	runErr := run()
	file.Close()
	out, readErr := os.ReadFile(name)
	if readErr != nil {
		t.Fatalf("read what it printed: %v", readErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, runErr
	}
	return strings.Split(trimmed, "\n"), runErr
}
