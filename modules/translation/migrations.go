package translation

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the translations table —
// owned as "translation". Nothing is adopted: the owner has no history under
// another name, so every installation that mounts this module applies 000043
// itself (see db.Adoption and docs/adr/0011).
//
// No RulesFrom floor either. A floor excuses files some installation already
// applied before a rewrite rule existed, and no installation ever applied a
// file of this one, so a floor here would excuse nothing and would be a number
// somebody had to keep honest.
var Migrations = db.MigrationSource{
	Owner:  "translation",
	Files:  db.Sub(schema, "migrations"),
	Adopts: nil,
}
