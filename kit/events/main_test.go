package events_test

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestMain isolates database-wide advisory locks from other packages; schema
// names do not scope the durable keys held by delivery and ledger moves.
func TestMain(m *testing.M) {
	os.Exit(dbtest.RunInOwnDatabase(m))
}
