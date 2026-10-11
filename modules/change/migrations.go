package change

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this module's SQL as one source — the proposals table — owned as
// "change". The version number is new and adopted by nobody: no installation ever
// applied these bytes as anything but a table nobody had, because nothing here
// existed before this module, and apps/platformkit's legacy-layout fixture takes
// the old ledger only as far as the highest file the foundation itself shipped
// (legacyLayout's preSplitTop). 000038 sits above that, so the fixture never
// claims it and there is no row for an adoption to re-own. The same reasoning
// covers modules/audit's 000035 to 000037 and modules/site's 000039; what sits
// *below* the foundation's own highest is what has to be adopted, which is why
// modules/auth names 31, 32 and 33 now that the kernel ships 000034 above them.
//
// No RulesFrom either: every file this source carries is guarded by the rule
// table, which arrived before any of it.
var Migrations = db.MigrationSource{
	Owner: "change",
	Files: db.Sub(schema, "migrations"),
}
