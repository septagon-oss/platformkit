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
var Migrations = db.MigrationSource{
	Owner:  "audit",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{10, 15, 23}}},
	// Two applied files index audit_events in a file that does not create it
	// (000015, 000023), which is the shape the guard refuses. They are history
	// and cannot be rewritten, so the guard starts past them.
	RulesFrom: 24,
}
