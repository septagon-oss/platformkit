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
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{7, 25, 30}}},
	// 000025 indexes users twice in a file that does not create it. Applied, so
	// unmarkable; the guard takes version 26 onwards.
	RulesFrom: 26,
}
