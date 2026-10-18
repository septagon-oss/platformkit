package events_test

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestMain gives this package a database of its own, for the reason dbtest.RunInOwnDatabase gives
// and one of its own besides. MoveLedger asks an exclusive advisory lock per unscoped durable it is
// about to rename and refuses rather than waits (kit/events/ledger.go's holdDurable), and
// migrations/000058_ledger_claim_durable_lock.up.sql makes every INSERT of an unscoped claim take
// the matching shared lock for its whole transaction. Both keys are the durable's own text hashed
// into one database's advisory space, and a schema separates no two holders of such a key. Every
// package of the suite was handed the same database, and two of them plant their test ledgers at one
// unscoped durable — kit/app among them, whose TestARefusedLedgerMoveCannotReopenAlreadyHandledWork
// holds a claim on it open for most of its run — so the package that reached its claim first held
// the key the other's move was asking for, and that move refused over a delivery no test had
// opened. Holding that one key from a second session brings the refusal the suite reported, word for
// word, at moved_ledger_tenant_write_test.go:25; the same case passes with the key held in some other
// database, which is what this line buys.
func TestMain(m *testing.M) {
	os.Exit(dbtest.RunInOwnDatabase(m))
}
