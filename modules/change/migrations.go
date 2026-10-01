package change

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the proposals table — owned as
// "change". The version number is new rather than adopted: nothing ever applied
// these bytes under another owner, because nothing here existed before this
// module. The manifest therefore declares no Adopts and no RulesFrom, which is the
// manifest's own rule for a module added after the rule table existed: every file
// it carries is guarded by it, and it should be.
var Migrations = db.MigrationSource{
	Owner: "change",
	Files: db.Sub(schema, "migrations"),
}
