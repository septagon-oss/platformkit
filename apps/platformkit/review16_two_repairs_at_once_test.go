package main

// Review 16's pins. Two claims of this change had no case behind them.
//
// One is the brief's invariant read off the running installation: acme's
// administrator holds the wildcard and the operator permissions of the manifests
// compose lists — and nothing beside them. The expected set is derived from
// c.modules rather than from composition.catalogue, the closure the seeder is
// handed, so it is a second derivation and not the same expression read back.
// Measured here, it is a weak lock by construction: the only operator
// permissions any module of this repository declares are tenant:manage and
// billing:catalog, which is exactly the hand-written literal this task removed,
// so a reverted literal reads equal at this composition. It pins the equality;
// modules/auth/seed_composition_test.go, with its two compositions differing by
// one module, is what reddens a hand-written list.
//
// The other is the sentence in roles.go: "two operators running this at once —
// both reading the same dead grant before either reached it — print one line
// between them and not two". No committed case reaches that branch under true
// simultaneity: TestTwoRepairsOfOneRoleAreOneWrite orders its runs, and
// TestARepairThatWroteNoRowReportsNoRemoval reaches wrote=false by running one
// repair after another. Both are correct and neither is the race.
//
// So this runs the race. The administration advisory lock the write takes
// (internal.administrationLock) is held by a third transaction while both runs
// read the tenant, so both read the dead grant and both then queue on that
// lock: the first writes the row and publishes, the second re-reads under the
// lock, finds the list it was handed, writes nothing, publishes nothing and must
// say nothing. pg_locks says when both are queued — the probe never waits on
// anything the command prints. Every assertion is a row, an outbox count, or
// the number of lines claiming a removal.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestTwoOperatorsRunningTheRepairAtOncePrintOneRemoval(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var acme tenancy.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		acme, e = c.tenants.ByHost(ctx, tx, acmeHost)
		return e
	}); err != nil {
		t.Fatalf("read the bootstrapped tenant: %v", err)
	}

	// What the composition declares as operator, derived from the module list
	// compose returned rather than from the catalogue closure it seeds with.
	var declares []string
	for _, m := range c.modules {
		for _, p := range m.Permissions {
			if p.Operator {
				declares = append(declares, p.Key)
			}
		}
	}
	slices.Sort(declares)
	if len(declares) == 0 {
		t.Fatal("the composition this file writes declares no operator permission, so the comparison below proves nothing")
	}

	// The administrator the bootstrap seeded, read back through the tenant's own
	// policy: the wildcard and these permissions, with nothing beside them.
	admin := rolesOf(t, c, conn, acme)[authcontracts.RoleAdmin]
	if !slices.Contains(admin, "*") {
		t.Fatalf("acme's administrator was seeded %v, want the wildcard", admin)
	}
	held := []string{}
	for _, p := range admin {
		if p != "*" {
			held = append(held, p)
		}
	}
	slices.Sort(held)
	if !slices.Equal(held, declares) {
		t.Errorf("acme's administrator holds %v; the modules this composition lists declare %v as operator "+
			"— a grant in the first list that is not in the second is the defect this task was written for", held, declares)
	}

	// The row an older seeder left, which no door would write.
	plant(t, conn, acme, authcontracts.RoleAdmin, "ghost:read")
	before := countRows(t, conn, "platformkit_outbox")

	// The lock the write takes, and the halves pg_locks splits a 64-bit
	// advisory key into.
	key := "administration/" + acme.ID.String()
	var hash int64
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT hashtextextended(?, 0)", key).Scan(&hash).Error
	}); err != nil {
		t.Fatalf("hash the lock key: %v", err)
	}
	classid := uint32(uint64(hash) >> 32)
	objid := uint32(uint64(hash) & 0xffffffff)
	// The runs queued on that key, and whether the holder holds it: a fact of
	// the lock manager, never of anything the command prints.
	locks := func(granted bool) int {
		var n int
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Raw(`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory'
				AND granted = ? AND classid = ?::oid AND objid = ?::oid`, granted, classid, objid).Scan(&n).Error
		})
		if err != nil {
			t.Fatalf("count the locks held or queued on the tenant's role lock: %v", err)
		}
		return n
	}

	release := make(chan struct{})
	holderDone := make(chan struct{})
	var letGo sync.Once
	go func() {
		defer close(holderDone)
		_ = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if e := tx.DB().Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", key).Error; e != nil {
				return e
			}
			<-release
			return nil
		})
	}()
	defer func() { letGo.Do(func() { close(release) }); <-holderDone }()

	// Nothing may start until the holder actually holds.
	heldLock := false
	for range 120 {
		if locks(true) > 0 {
			heldLock = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !heldLock {
		t.Fatal("the transaction meant to hold this tenant's role lock never appeared in pg_locks: the harness, not the command")
	}

	// Two runs, one output stream, both behind the held lock.
	name := filepath.Join(t.TempDir(), "printed")
	file, err := os.Create(name)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	saved := os.Stdout
	os.Stdout = file
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repairRoles([]string{"--config", path, "--remove"})
		}()
	}
	queued := 0
	for range 240 {
		if queued = locks(false); queued >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("runs queued on the tenant's role lock at once: %d of 2", queued)
	letGo.Do(func() { close(release) })
	wg.Wait()
	file.Close()
	os.Stdout = saved
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("the two runs returned %v and %v, want both to finish", errs[0], errs[1])
	}
	if queued < 2 {
		t.Log("the race was not exercised this time; the assertions below still hold of what did happen")
	}

	out, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read what they printed: %v", err)
	}
	lines := slices.DeleteFunc(strings.Split(strings.TrimSpace(string(out)), "\n"),
		func(line string) bool { return strings.TrimSpace(line) == "" })
	// The claim under test: one removal, told once. Counted through the row key
	// the command prints for a grant it took, which the correct run prints too.
	removals := []string{}
	for _, line := range lines {
		if strings.Contains(line, "\tremoved\t") {
			removals = append(removals, line)
		}
	}
	if len(removals) != 1 {
		t.Errorf("the two runs printed %d removal lines (%q), want exactly one between them: "+
			"the run whose write found the row already holding the list it was handed changed no row and publishes no event, "+
			"so it must not say it removed anything", len(removals), lines)
	} else if !strings.HasPrefix(removals[0], "acme\tadmin\tremoved\t") || !strings.Contains(removals[0], "ghost:read") {
		t.Errorf("the one removal line reads %q, want it to name acme's administrator and ghost:read", removals[0])
	}
	for _, line := range lines {
		if strings.Contains(line, "nothing for this run") && strings.Contains(line, "ghost:read") {
			t.Errorf("the all-clear names the grant that is gone: %q", line)
		}
	}

	if after := rolesOf(t, c, conn, acme)[authcontracts.RoleAdmin]; slices.Contains(after, "ghost:read") {
		t.Errorf("ghost:read is still in acme's administrator after the two runs: %v", after)
	} else if !slices.Contains(after, "*") {
		t.Errorf("the repair took the wildcard as well: %v", after)
	}
	// One write between them, and one event: the run that wrote no row emits
	// nothing, whichever of the two it was.
	if after := countRows(t, conn, "platformkit_outbox"); after != before+1 {
		t.Errorf("the two runs published %d outbox rows over the %d already there, want exactly one for the one row they moved", after, before)
	}
}

func countRows(t *testing.T, conn *db.Conn, from string) int {
	t.Helper()
	var n int
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw("SELECT count(*) FROM " + from).Scan(&n).Error
	})
	if err != nil {
		t.Fatalf("count %s: %v", from, err)
	}
	return n
}
