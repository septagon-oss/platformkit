package tours

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations owns the tenant-scoped tours runtime schema.
var Migrations = db.MigrationSource{Owner: "tours", Files: db.Sub(schema, "migrations")}
