package auth

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — sessions, roles, password and verification tokens — owned as
// "auth". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, auth.Migrations).
//
// The files keep the version numbers they had when the foundation applied
// them under its own name, so an installation migrated before this module
// owned its SQL is adopted by checksum rather than migrated again (see
// db.Adoption and docs/adr/0011). New files continue from the highest number.
var Migrations = db.MigrationSource{
	Owner: "auth",
	Files: db.Sub(schema, "migrations"),
	// 31, 32 and 33 are this module's own files, written after the split, and they
	// are adopted for one reason that has nothing to do with their content: the
	// kernel ships 000034_outbox_request above them, and apps/platformkit's
	// legacy-layout fixture takes its ledger down to the highest file the
	// foundation itself ships. So those three versions arrive in the fixture under
	// `platformkit`, and kit/db refuses an applied version this release no longer
	// carries under that name. Adoption matches by checksum, the bytes are this
	// module's own, and a real installation that never saw them applies them here.
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{8, 13, 14, 24, 31, 32, 33}}},
	// 000013 drops a column, adds a NOT NULL column with no default and indexes
	// sessions — every one of them a rewrite or a lock, all of them applied years
	// ago under bytes that cannot change. The guard takes version 14 onwards.
	RulesFrom: 14,
}
