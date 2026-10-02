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
	//
	// 35, 36 and 37 are adopted by nobody, and that is the whole point of the
	// number they carry. apps/platformkit's legacy-layout fixture models what an
	// installation's ledger looks like before modules owned their SQL: it
	// flattens the files that existed then under the one owner the foundation
	// used, and it stops at the highest file the foundation itself shipped
	// (legacyLayout's preSplitTop, 000034 today). A file above that number
	// postdates the split, so the old ledger must not claim it was applied —
	// there would be no row to re-own and kit/db would be right to call an
	// applied file no release ships a contradiction. Those three therefore apply
	// normally in the upgrade, under "audit", from the first commit that ships
	// them, and an adoption naming them would name a row no installation holds.
	//
	// The same rule is why they are 35 and not the 24 this owner's own sequence
	// had reached: one file per version holds across the whole composition, so a
	// new migration in any module takes a version no other owner has taken.
	RulesFrom: 24,
}
