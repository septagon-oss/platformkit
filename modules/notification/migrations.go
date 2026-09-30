package notification

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the notifications table — owned as
// "notification". The manifest hands the kernel the same files and adoption, and a test
// composes the schema it needs beside the foundation: dbtest.Schema(t, notification.Migrations).
//
// Every version this source ships is adopted from "platformkit", the owner that
// applied the pre-split files under its own name: a ledger row naming it re-owns by
// checksum rather than stranding the file, and where no such row exists — every
// newer file, and every fresh installation — the adoption does nothing (docs/adr/0011).
var Migrations = db.MigrationSource{
	Owner:  "notification",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{11, 27, 28, 29, 30}}},
}
