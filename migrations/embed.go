// Package migrations owns the kernel's schema: the tenancy helper functions and
// the tenants and hosts they resolve, the outbox with its claims and dead
// letters, and the limits ledger. Everything a module stores lives beside that
// module, under modules/<name>/migrations, and the module's manifest hands it
// to the kernel; kit/app puts this source first and the modules after it in
// composition order. Files keep the numbers they were applied under, with the
// gaps the modules took with them.
package migrations

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed *.up.sql
var files embed.FS

// Source is the kernel's append-only migration history, owned as "platformkit".
var Source = db.MigrationSource{Owner: "platformkit", Files: files}
