// Package migrations owns the foundation schema shared by PlatformKit apps.
package migrations

import (
	"embed"

	"github.com/septagon-oss/platformkit/kit/db"
)

//go:embed *.up.sql
var files embed.FS

// Source is the foundation's append-only migration history.
var Source = db.MigrationSource{Owner: "platformkit", Files: files}
