package file

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the files table — owned as
// "file". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, file.Migrations).
//
// The files keep the version numbers they had when the foundation applied
// them under its own name, so an installation migrated before this module
// owned its SQL is adopted by checksum rather than migrated again (see
// db.Adoption and docs/adr/0011). New files continue from the highest number.
//
// 19 is the only adoption: it is the one file this source holds that a release
// of the foundation applied under its own name. The retention file is 34,
// numbered past the highest any owner had shipped when it was written, so no
// installation ever saw it under "platformkit" and nothing declares it here —
// the upgrade fixture (apps/platformkit/app_test.go, legacyLayout) flattens
// every owner's files under the foundation's name up to the foundation's own
// highest, and a file above that number is simply new work the old ledger does
// not claim.
var Migrations = db.MigrationSource{
	Owner:  "file",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{19}}},
}
