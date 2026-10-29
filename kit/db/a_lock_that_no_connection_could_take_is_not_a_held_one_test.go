package db_test

// An advisory lock answers two questions, and ErrHeld is the answer to exactly one of
// them: "another session holds this key". The other — no connection to hold a key on, the
// server gone, the pool ended — has to come back as its own error, because a caller that
// hears it as a peer says the work is happening elsewhere and waits for a replica that
// never existed. The pair of answers is read by the job scheduler's two outcomes, so the
// distinction belongs to this package and is asserted here where the distinction is made.

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTryLockErrOnAPoolThatHasNoConnectionIsNotErrHeld: an ended pool answers with its own
// error and no release, never ErrHeld, and TryLock reports the failure rather than the
// "not taken" a holder produces — the two answers a scheduler must not collapse into one.
func TestTryLockErrOnAPoolThatHasNoConnectionIsNotErrHeld(t *testing.T) {
	_, appURL := dbtest.URLs(t)
	conn := poolFor(t, appURL)
	if err := conn.Close(); err != nil {
		t.Fatalf("end the pool a lock would take its key on: %v", err)
	}
	ctx := t.Context()

	unlock, err := db.TryLockErr(ctx, conn, "no_connection_to_hold")
	if err == nil {
		t.Fatal("a lock against an ended pool reported success")
	}
	if errors.Is(err, db.ErrHeld) {
		t.Errorf("a lock nobody could take answers ErrHeld (%v); that answer says a peer holds the key", err)
	}
	if unlock != nil {
		t.Errorf("a lock that was never taken handed back a release: %v", unlock)
	}

	legacyUnlock, taken, legacyErr := db.TryLock(ctx, conn, "no_connection_to_hold")
	if legacyErr == nil {
		t.Fatal("the old door reported no failure against an ended pool")
	}
	if errors.Is(legacyErr, db.ErrHeld) {
		t.Errorf("the old door answered ErrHeld (%v) for a lock nobody could take; its caller reads that as a peer", legacyErr)
	}
	if taken || legacyUnlock != nil {
		t.Errorf("TryLock answered a lock whose connection could not be taken as taken: ok=%v unlock=%v", taken, legacyUnlock != nil)
	}
}
