package audit

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the audit trail and its record index — owned as
// "audit". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, audit.Migrations).
//
// The files keep the version numbers they had when the foundation applied
// them under its own name, so an installation migrated before this module
// owned its SQL is adopted by checksum rather than migrated again (see
// db.Adoption and docs/adr/0011). New files continue from the highest number
// anywhere in the composition, which is why the trace column is 000035 and not
// the 000030 it was first written at: the kernel took that number for
// 000030_tenant_oidc while this branch was open, and `kit/db` refuses one owner
// shipping two files at a version — which is exactly what the upgrade fixture
// (apps/platformkit/app_test.go, legacyLayout) would have asked it to do, since
// that fixture flattens every owner's files under the kernel's name up to the
// kernel's own highest version. At 000035 the file postdates the split, so the
// fixture leaves it out of the old ledger, the upgrade applies it as a new row,
// and nothing is adopted at 30.
var Migrations = db.MigrationSource{
	Owner:  "audit",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{10, 15, 23}}},
	// Two applied files index audit_events in a file that does not create it
	// (000015, 000023), which is the shape the guard refuses. They are history
	// and cannot be rewritten, so the guard starts past them.
	RulesFrom: 24,
}
