package app

import (
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/migrations"
)

// MigrationSources keeps the foundation first and modules in composition order.
// Module names own their histories; no global version allocation is needed.
func MigrationSources(mods []module.Module) []db.MigrationSource {
	sources := []db.MigrationSource{migrations.Source}
	for _, mod := range mods {
		if mod.Migrations != nil {
			sources = append(sources, db.MigrationSource{Owner: mod.Name, Files: mod.Migrations})
		}
	}
	return sources
}
