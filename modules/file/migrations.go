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
// of the foundation applied under its own name. The retention file is 40,
// numbered past the highest any owner ships, so no installation ever saw it
// under "platformkit" and nothing declares it here — the upgrade fixture
// (apps/platformkit/app_test.go, legacyLayout) flattens every owner's files
// under the foundation's name up to the foundation's own highest, and a file
// above that number is simply new work the old ledger does not claim.
//
// It was written as 000034 and moved to 000040 in the merge that brought the
// foundation's own migrations/000034_outbox_request into the same tree: one
// version is one ledger row per owner, and the fixture's flattening puts two
// owners' 34s under one name, which kit/db refuses before the ledger ever sees
// it (migration_files.go, "invalid or repeated version"). The rule the number
// follows is the one this source already followed when it left 000030 behind:
// continue past the highest number anywhere in the composition. As this tree
// stands that is modules/site's 000039, so the retention file is 40.
var Migrations = db.MigrationSource{
	Owner:  "file",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{19}}},
}
