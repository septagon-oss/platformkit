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
)

// TestTheRepairCommandPrintsWhatItCommitted reads what repair-roles printed, the
// half of the command no other case looks at: the listing is how the operator
// decides, so a run that changed rows without naming them — or named a row its
// transaction then failed to commit — failed at the one thing it is for.
// TestTheRepairCommandListsBeforeItRemoves reads the rows and says so itself;
// nothing read the printing. Every line here is checked against the row it
// claims: still there after a listing, gone after --remove, and a second run has
// nothing left to claim about it.
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
		var e error
		if acme, e = c.tenants.ByHost(ctx, tx, acmeHost); e != nil {
			return e
		}
		return tx.DB().Exec(
			"UPDATE roles SET permissions = array_append(permissions, 'ghost:read') WHERE tenant_id = ? AND name = ?",
			acme.ID, authcontracts.RoleAdmin).Error
	})
	if err != nil {
		t.Fatalf("leave the row an older seeder left: %v", err)
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

	// os.Stdout is the command's only output channel, so the case takes it — to a
	// file rather than a pipe, because the writer is this same process and a pipe
	// would fill and wait, and with the swap undone by a defer, so that a case
	// that fails while holding it does not send its own report to it.
	printed := func(run func() error) []string {
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
		if runErr != nil {
			t.Fatalf("repair-roles: %v", runErr)
		}
		if readErr != nil {
			t.Fatalf("read what it printed: %v", readErr)
		}
		return strings.Split(strings.TrimSpace(string(out)), "\n")
	}

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
		!strings.Contains(lines[0], "every grant this installation's seeder wrote") {
		t.Errorf("a second run printed %q, want the sentence that says there is nothing left to take", lines)
	}
}
