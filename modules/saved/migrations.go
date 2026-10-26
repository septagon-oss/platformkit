// Package saved is the module manifest: what the kernel needs to know about
// retained personal copies that it cannot learn from a function call.
//
// It follows the shape every module follows — contracts/, internal/, and this
// package: the schema this package hands the migrator and the pkit declaration in
// module.go of what the composition may ask for.
package saved

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the saved_items table and its
// three indexes — owned as "saved". A test composes the schema it needs beside
// the foundation with dbtest.Schema(t, saved.Migrations).
//
// Its number, 75, is the first the foundation's history had not taken when this
// table was composed, and the foundation holds its own file at that number: the
// ledger keys on (owner, version), so two owners may hold one version. What no
// release may do is stop listing a file it shipped under "saved" — the migrator
// refuses an applied file its source no longer names, and history keeps its name.
var Migrations = db.MigrationSource{
	Owner: "saved",
	Files: db.Sub(schema, "migrations"),
}
