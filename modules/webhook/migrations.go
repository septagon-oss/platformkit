package webhook

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed migrations/*.up.sql
var schema embed.FS

// Migrations is this capability's SQL as one source — the endpoint, its secret,
// the delivery log — owned as "webhook".
//
// The version numbers are new and adopted by nobody: no installation ever applied
// these bytes as anything but tables nobody had, because nothing here existed
// before this module. They sit above the highest file the foundation itself
// shipped (migrations/000043_tenant_app.up.sql), which is the condition
// apps/platformkit's legacy-layout fixture checks — see modules/change/migrations.go
// for the reasoning, which is the same reasoning and is written down there once.
//
// No RulesFrom either: every file this source carries is guarded by the rule
// table, which arrived before any of it.
var Migrations = db.MigrationSource{
	Owner: "webhook",
	Files: db.Sub(schema, "migrations"),
}
