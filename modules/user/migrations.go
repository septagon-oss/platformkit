package user

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the users table — owned as
// "user". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, user.Migrations).
//
// The files keep the version numbers they had when the foundation applied
// them under its own name, so an installation migrated before this module owned
// its SQL adopts them by checksum (see db.Adoption and docs/adr/0011) — and a new
// file is named here too, because that ledger holds every file under one owner.
var Migrations = db.MigrationSource{
	Owner:  "user",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{7, 25}}},
	// 000072 and 000073 are deliberately absent from that list. They are files
	// this release ships new, not history the foundation applied, and an adoption
	// is a claim about the old ledger: apps/platformkit's upgrade fixture builds
	// its old installation out of every version up to the highest adopted one, so
	// naming 72 and 73 here would drag every post-split file of every owner below
	// them into a ledger that never saw them — and their numbers are 72 and 73
	// rather than the 30 and 32 they were first written as because an adoption
	// also matches on owner and version: 30 here would find the kernel's own
	// 000030_tenant_oidc and refuse the upgrade over a file this module had never
	// shown. A new file takes a number above every file in the composition,
	// wherever it lives (apps/platformkit/app_test.go counts them).
	//
	// 000025 indexes users twice in a file that does not create it. Applied, so
	// unmarkable; the guard takes version 26 onwards.
	RulesFrom: 26,
}
