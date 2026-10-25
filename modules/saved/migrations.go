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
// The file carries the version number the foundation's own history had reached
// when this table was written, so the version is new to every installation
// rather than adopted from a time when saved's SQL belonged to somebody else.
var Migrations = db.MigrationSource{
	Owner: "saved",
	Files: db.Sub(schema, "migrations"),
}
