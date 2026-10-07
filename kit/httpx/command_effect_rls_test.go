package httpx_test

// The effect marker is the one kernel table a tenant transaction writes, and it is
// written by necessity: a marker that sat outside the transaction it describes would
// say nothing about whether that transaction committed. The widening is real — see
// migrations/000045_command_effect.up.sql for what it costs and for what a forged row
// can buy — and this file is its two bounds. A tenant's transaction may place a marker
// for its own tenant's keys and can neither read nor place one for another tenant's;
// and the marker is inside the transaction, which means a command the database rolled
// back leaves nothing behind for the next press to mistake for a committed write.

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const effectTable = "platformkit_command_effect"

func TestAnotherTenantCanNeitherReadNorWriteACommandEffect(t *testing.T) {
	_, router, f, _, path := setupNote(t)
	if res := send(t, router, path, keyA, "private effect"); res.Code != http.StatusOK {
		t.Fatalf("the keyed command answered %d: %s", res.Code, res.Body)
	}
	// The marker is a tenant table, so the tenant's own transaction reads it back: one
	// submission that committed is one marker, and countRows is the tenant's door.
	if n := countRows(t, f, effectTable); n != 1 {
		t.Fatalf("one committed command left %d effect markers; want one", n)
	}
	other := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "other"})
	var seen int
	if err := db.Run(other, f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM "+effectTable+" WHERE tenant_id = ?", f.tenant.ID).Row().Scan(&seen)
	}); err != nil {
		t.Fatalf("the other tenant could not ask: %v", err)
	}
	if seen != 0 {
		t.Errorf("another tenant read %d of the first tenant's effect markers", seen)
	}
	// A separate transaction, because a refused statement aborts the one it sat in:
	// the policy's own answer is what is being asked for here, not what follows it.
	var written int64
	err := db.Run(other, f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec("INSERT INTO "+effectTable+
			" (tenant_id, actor_id, operation, key) VALUES (?, ?, 'POST /notes', ?)",
			f.tenant.ID, uuid.New(), keyA)
		written = res.RowsAffected
		return res.Error
	})
	if err == nil || written != 0 {
		t.Errorf("another tenant placed %d markers in a tenant that is not its own (error %v)", written, err)
	}
	if n := countRows(t, f, effectTable); n != 1 {
		t.Errorf("the first tenant's marker is now %d rows: it was read or replaced across the tenant", n)
	}
}

// TestACommandTheDatabaseRolledBackLeavesNoEffectMarker is the half of the marker that
// makes it worth anything: it commits with the effect and rolls back with it, with no
// fault injected in the acknowledgement at all. A refusal that rolled the work back
// leaves no marker and no claim, so the same key runs; when the same command does
// commit, its marker is the one row the next press will read.
func TestACommandTheDatabaseRolledBackLeavesNoEffectMarker(t *testing.T) {
	api, router, f, runs, _ := setupNote(t)
	const keyFail = "3f2504e0-4f89-41d3-9a0c-0305e82c3311"
	refused := errors.New("this command refused")
	fail := &atomic.Bool{}
	writeNote := note(runs)
	mount(t, api, "failing-note", "/failing-notes", true,
		func(ctx context.Context, in *noteIn) (*noteOut, error) {
			out, err := writeNote(ctx, in)
			if err != nil {
				return nil, err
			}
			if fail.Load() {
				return nil, refused
			}
			return out, nil
		})
	path := at(api, "/failing-notes")

	fail.Store(true)
	first := send(t, router, path, keyFail, "rolled back with its marker")
	if first.Code < http.StatusInternalServerError {
		t.Fatalf("a command that failed after its write answered %d, want 5xx: %s", first.Code, first.Body)
	}
	if n := countRows(t, f, "notes"); n != 0 {
		t.Fatalf("the rolled-back command left %d notes", n)
	}
	if n := countRows(t, f, effectTable); n != 0 {
		t.Errorf("the rolled-back command left %d effect markers; want none, because the marker is inside the transaction", n)
	}
	if n := heldRows(t, f, true); n != 0 {
		t.Fatalf("the rolled-back command held %d claims; want none", n)
	}

	fail.Store(false)
	retry := send(t, router, path, keyFail, "rolled back with its marker")
	if retry.Code != http.StatusOK {
		t.Fatalf("the same key after a rollback answered %d, want 200: %s", retry.Code, retry.Body)
	}
	if n := countRows(t, f, "notes"); n != 1 {
		t.Fatalf("the retry committed %d notes; want one", n)
	}
	if n := countRows(t, f, effectTable); n != 1 {
		t.Errorf("the command that committed left %d effect markers; want one", n)
	}
}
