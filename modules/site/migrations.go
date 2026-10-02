package site

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the site settings table — owned as
// "site". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, site.Migrations).
//
// The files keep the version numbers they had when the foundation applied
// them under its own name, so an installation migrated before this module
// owned its SQL is adopted by checksum rather than migrated again (see
// db.Adoption and docs/adr/0011). New files continue from the highest number —
// 000039 and 000041 are two, and neither is adopted by anybody: apps/platformkit's
// legacy-layout fixture takes the old ledger only as far as the highest file the
// foundation itself shipped, so a module file above that postdates the split,
// applies under its own owner, and has no row for an adoption to name.
// 000041 is 41 rather than the 19 the file was written as because the sequence runs
// across every owner, not inside one module: 19 belongs to modules/file's own
// 000019_file, and this composition flattens both owners' files into one ledger, so
// a second 19 is refused as a repeated version before any SQL runs. modules/change's
// migrations.go gives the same reason at length.
var Migrations = db.MigrationSource{
	Owner:  "site",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{18}}},
}
