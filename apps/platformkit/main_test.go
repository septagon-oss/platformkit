package main

import (
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestMain gives this package a database of its own.
//
// Every case here installs an application — an empty schema, the whole ledger, a tenant and an
// administrator — because that is the claim the package exists to prove: the README's third command
// run exactly as a person runs it. A migration run holds one advisory lock for its whole
// composition, an advisory lock belongs to a database, and `go test ./...` runs every other package
// that migrates against the database the test URLs name. Without this line the package's own
// migrations queue behind every other package's, and a whole-suite run reaches the bound its
// `go test` carries while it is still working — six passing cases then reported as `(unknown)` over
// a dump of tests parked in the lock. The bound is not what failed; the queue is. The same
// reasoning, the same remedy and the same owner are `kit/db`'s TestMain.
func TestMain(m *testing.M) {
	os.Exit(dbtest.RunInOwnDatabase(m))
}
