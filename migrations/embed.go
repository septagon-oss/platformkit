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
//
// RulesFrom is 21 because two files below it index a table another file created
// (000012 on tenants, 000020 on tenant_hosts) and were applied under those bytes
// long before the rule table existed: they cannot be marked, because marking them
// would mean changing bytes some installation already applied. Version 21 creates
// and indexes its own table, so the guard is whole from there on. A new kernel
// file is guarded, and moving this number down is a review, not an edit.
var Source = db.MigrationSource{Owner: "platformkit", Files: files, RulesFrom: 21}
