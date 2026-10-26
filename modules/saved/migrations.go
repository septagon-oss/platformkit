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
// The file carries the first version number the foundation's own history had not
// taken when this table was composed. A version is the migrator's key: two owners
// at one number means the second one's SQL is never applied, and the applied
// census then reads the first owner's name where the second's table stands.
// Composing this module is what made the collision visible.
var Migrations = db.MigrationSource{
	Owner: "saved",
	Files: db.Sub(schema, "migrations"),
}
