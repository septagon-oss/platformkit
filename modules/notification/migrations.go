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
// Every version this source shipped before the split is adopted from
// "platformkit", the owner that applied the pre-split files under its own name: a
// ledger row naming it re-owns by checksum rather than stranding the file, and where
// no such row exists — every fresh installation — the adoption does nothing
// (docs/adr/0011). The four files this delivery adds are numbered past every file
// the release holds (000054 to 000057, past modules/audit's 000053), so no release
// before this one can have applied them and nothing adopts them back: they arrive as
// new rows on an upgraded installation, which is the shape apps/platformkit's upgrade
// case counts.
var Migrations = db.MigrationSource{
	Owner:  "notification",
	Files:  db.Sub(schema, "migrations"),
	Adopts: []db.Adoption{{Owner: "platformkit", Versions: []int64{11, 27}}},
}
