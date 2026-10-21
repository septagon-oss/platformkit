package decide

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations owns Decide's tenant-scoped aggregate and ballot constraints.
var Migrations = db.MigrationSource{Owner: "decide", Files: db.Sub(schema, "migrations")}
