// Package migrations is the one migration directory. Every table in PlatformKit
// is created here, numbered once, and applied by kit/db.Migrate as the owner
// role against the single "schema_migrations" ledger.
package migrations

import "embed"

// FS holds the migration files. kit/db.Migrate reads it through source/iofs.
//
//go:embed *.sql
var FS embed.FS
