package notificationtest_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

// TestTheFakeIsAMailLedger runs the mail ledger's conformance suite against the
// fake. The suite is the table's own rules — the outcome vocabulary, the kind
// grammar, "not sent means a reason", an address or nothing — so a fake that did
// not pass it would hand a caller a record Postgres would refuse, and the caller
// would find out against a database rather than in its own test.
//
// What the fake cannot prove is commit topology: that a record is written in the
// transaction of the send it describes and disappears with it. Those two cases
// live in modules/notification/internal, over a schema, because a slice has no
// transaction to abort.
func TestTheFakeIsAMailLedger(t *testing.T) {
	notificationtest.RunMailLedger(t, func(t *testing.T, run func(notificationtest.MailFixture)) {
		ledger := notificationtest.NewFakeMailLedger()
		run(notificationtest.MailFixture{
			Ctx:    t.Context(),
			Ledger: ledger,
			// A step is a transaction the fake does not have: it records what it
			// is handed and returns what the caller returned, refusal included.
			Step: func(ctx context.Context, fn func(context.Context, db.Tx[db.Tenant]) error) error {
				return fn(ctx, db.Tx[db.Tenant]{})
			},
			Rows: func(t testing.TB) []contracts.MailRecord { return ledger.Rows() },
		})
	})
}
