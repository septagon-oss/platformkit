package db_test

// TryLockErr answers the one question a lock caller has — ErrHeld, or the release —
// and TryLock now asks it: the pair is that the old door's three answers, and its
// failure sentence in particular, survive the delegation.

import (
	"errors"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// poolFor opens a pool of its own, so two holders are two sessions, not two handles.
func poolFor(t *testing.T, url string) *db.Conn {
	t.Helper()
	conn, err := db.OpenWithPool(t.Context(), url, db.Pool{MaxOpenConns: 2, MaxIdleConns: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// TestTryLockErrAnswersErrHeldWhereTryLockAnswersNotTaken: the holder keeps the lock
// while a second process is told so, the loser is handed no release, and the key is
// free once the holder is done.
func TestTryLockErrAnswersErrHeldWhereTryLockAnswersNotTaken(t *testing.T) {
	_, appURL := dbtest.URLs(t)
	holder, asker := poolFor(t, appURL), poolFor(t, appURL)
	ctx := t.Context()

	unlock, taken, err := db.TryLock(ctx, holder, "pair-held")
	if err != nil || !taken {
		t.Fatalf("the holder was refused: taken=%v err=%v", taken, err)
	}

	release, err := db.TryLockErr(ctx, asker, "pair-held")
	if !errors.Is(err, db.ErrHeld) {
		t.Fatalf("a second process asking for a held lock got %v, want errors.Is(err, db.ErrHeld)", err)
	}
	if release != nil {
		t.Errorf("the loser was handed an Unlock alongside ErrHeld; it holds no connection to release")
	}
	if _, ok, err := db.TryLock(ctx, asker, "pair-held"); err != nil || ok {
		t.Errorf("TryLock on the held name answered ok=%v err=%v, want the false it always answered", ok, err)
	}

	unlock()
	third, err := db.TryLockErr(ctx, asker, "pair-held")
	if err != nil {
		t.Fatalf("the name stayed held after the holder released it: %v", err)
	}
	third()
}

// TestTryLockFailureSentenceIsTheSentenceTryLockErrReturns: a job's log line is this
// package's own sentence, and a door that wrapped it again would double the prefix under
// a comment telling nobody to expect a new one.
func TestTryLockFailureSentenceIsTheSentenceTryLockErrReturns(t *testing.T) {
	_, appURL := dbtest.URLs(t)
	conn := poolFor(t, appURL)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	unlock, err := db.TryLockErr(t.Context(), conn, "pair-closed-pool")
	legacyUnlock, taken, legacyErr := db.TryLock(t.Context(), conn, "pair-closed-pool")
	if err == nil || legacyErr == nil {
		t.Fatalf("both doors reported success against a closed pool: %v, %v", err, legacyErr)
	}
	if err.Error() != legacyErr.Error() {
		t.Errorf("the old door says %q and the new one %q; the delegation rewrote the sentence", legacyErr, err)
	}
	if unlock != nil || legacyUnlock != nil || taken {
		t.Errorf("a failed lock reported itself taken: %v, %v, %v", unlock != nil, legacyUnlock != nil, taken)
	}
}

// TestUnlockReleasesOnceHoweverOftenItIsCalled: the release is guarded rather than
// harmless by accident, so a caller that hands one Unlock to two deferred paths neither
// ends the session twice nor leaves the key behind.
func TestUnlockReleasesOnceHoweverOftenItIsCalled(t *testing.T) {
	_, appURL := dbtest.URLs(t)
	holder, asker := poolFor(t, appURL), poolFor(t, appURL)
	ctx := t.Context()

	unlock, err := db.TryLockErr(ctx, holder, "pair-released-twice")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 3 {
		group.Add(1)
		go func() {
			defer group.Done()
			unlock()
		}()
	}
	group.Wait()

	release, err := db.TryLockErr(ctx, asker, "pair-released-twice")
	if err != nil {
		t.Fatalf("the key outlived its guarded release: %v", err)
	}
	release()
}
