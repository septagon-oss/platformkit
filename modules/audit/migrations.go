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
// db.Adoption and docs/adr/0011). New files continue from the highest number.
//
// 30 — the trace column — is in that list as well, and for a subtler reason than
// the three above it: no release ever shipped it under "platformkit", because it was
// written after the split. What makes the adoption real is the ledger an
// installation can arrive with. apps/platformkit's upgrade fixture flattens every
// owner's files under the kernel's name to reconstruct the pre-split layout, and the
// rule it checks is the one `kit/db` refuses a repeated version by: a source cannot
// ship two files at one version, so the flattened ledger — and any installation a
// migration tool of that shape touched — has 30 as "platformkit". Without the entry
// here, this source refuses to migrate at all: "000030_audit_trace.up.sql was applied
// but is missing from this release". With it, the checksum says whether the row and
// the file are the same migration, which is what an adoption has always meant.
var Migrations = db.MigrationSource{
	Owner:  "audit",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{10, 15, 23, 30}}},
	// Two applied files index audit_events in a file that does not create it
	// (000015, 000023), which is the shape the guard refuses. They are history
	// and cannot be rewritten, so the guard starts past them.
	RulesFrom: 24,
}
