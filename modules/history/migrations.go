package history

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the version table — owned as
// "history". A test composes the schema it needs beside the foundation with
// dbtest.Schema(t, history.Migrations, content.Migrations).
//
// The file opens this owner above any number the foundation applied under its own
// name, so it is a new row in the ledger and needs no adoption (see db.Adoption and
// docs/adr/0011). New files continue from the highest number in this directory.
var Migrations = db.MigrationSource{
	Owner: "history",
	Files: db.Sub(schema, "migrations"),
}
