package subject

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the request rows and the minted links —
// owned as "subject". It is the module's first version because the module is new: it
// adopts nothing, since nothing else ever owned a person's request.
//
// What this SQL creates names no person's rows. `subject_requests.subject_id` is the person
// the request is about and `requested_by` the one who asked, both of which the manifest
// files (module.go) — and both of which outlive the strings they name, because the record
// that a person asked, and that the tenant answered, has to survive the erasure it records.
var Migrations = db.MigrationSource{
	Owner: "subject",
	Files: db.Sub(schema, "migrations"),
}
