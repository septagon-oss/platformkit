package scim

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source, owned as "scim". Every file is
// new to this owner, so every file is guarded by the rule table: RulesFrom is 0
// and no adoption is declared, which is what a module that arrived after the
// foundation stopped handing out its own name has to look like.
var Migrations = db.MigrationSource{
	Owner:     "scim",
	Files:     db.Sub(schema, "migrations"),
	Adopts:    nil,
	RulesFrom: 0,
}
