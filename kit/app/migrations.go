package app

import (
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/migrations"
)

// MigrationSources keeps the foundation first and modules in composition order.
// Module names own their histories; no global version allocation is needed. A
// module's Adopts and its rule floor travel with its files: an installation migrated
// before the module owned its SQL is re-owned on the way past and nothing re-runs, and
// versions already applied under those bytes are not judged by a rule they cannot fix.
func MigrationSources(mods []module.Module) []db.MigrationSource {
	sources := []db.MigrationSource{migrations.Source}
	for _, mod := range mods {
		if mod.Migrations != nil {
			sources = append(sources, db.MigrationSource{Owner: mod.Name, Files: mod.Migrations, Adopts: mod.Adopts, RulesFrom: mod.RulesFrom})
		}
	}
	return sources
}
