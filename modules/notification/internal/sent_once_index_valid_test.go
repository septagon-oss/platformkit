package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestTheSentOnceIndexTheHistoryCertifiesIsValid pins the one database object
// the module's "a channel is sent once" answer rests on.
//
// migrations/000030 builds it with CREATE INDEX CONCURRENTLY IF NOT EXISTS in an
// autocommit file, which is the only shape a CONCURRENTLY build can have and the
// shape with a trap in it: a build that is cancelled while it waits leaves an
// index under the name it was building with indisvalid = false, IF NOT EXISTS
// then reads that name as "already done" and answers the re-run with a NOTICE and
// no error, and kit/db's apply records the file's history row for a statement
// that answered "skipping" (kit/db/migrate.go: apply returns only on execErr).
// What is left is an installation whose migration history certifies the
// sent-once constraint, whose index enforces nothing, and whose every ledger
// write with ON CONFLICT (notification_id, channel) WHERE outcome = 'sent'
// answers SQLSTATE 42P10 — measured in
// $STATE/scratch, and the reason the same build under `make check`'s five-second
// lock_timeout is the flakiest file in the suite.
//
// So: a history row for 000030 is only worth what the index behind it is, and
// this asks the database which of the two it has.
func TestTheSentOnceIndexTheHistoryCertifiesIsValid(t *testing.T) {
	_, conn := dbtest.Schema(t, notification.Migrations)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var (
			name      string
			valid, ok bool
		)
		row := tx.DB().Raw(`
			SELECT c.relname, i.indisvalid, i.indisunique
			FROM pg_index i
				JOIN pg_class c ON c.oid = i.indexrelid
				JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = 'notification_deliveries_sent_once'`).Row()
		if err := row.Scan(&name, &valid, &ok); err != nil {
			t.Errorf("000030's statement ran for this schema, and the index it names is not there at all: %v", err)
			return nil
		}
		if !valid {
			t.Errorf("%s is invalid: the ledger's ON CONFLICT (notification_id, channel) WHERE outcome = 'sent' "+
				"will answer 42P10 and the sent-once rule enforces nothing, while the history row says it holds", name)
		}
		if !ok {
			t.Errorf("%s is not unique, so it cannot refuse a second sent row for one channel", name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the sent-once index: %v", err)
	}
}
