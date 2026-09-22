package dbtest_test

import (
	"database/sql"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// Callers may retain these public helpers as function values. Widening their
// parameters to testing.TB breaks that code even though direct calls compile.
var (
	_ func(*testing.T) (string, string)                           = dbtest.URLs
	_ func(*testing.T, string) *sql.DB                            = dbtest.Open
	_ func(*testing.T, ...db.MigrationSource) (*sql.DB, *db.Conn) = dbtest.Schema
	_ func(*testing.T, string) string                             = dbtest.RoleOf
	_ func(testing.TB) (string, string)                           = dbtest.URLsFor
	_ func(testing.TB, string) *sql.DB                            = dbtest.OpenFor
	_ func(testing.TB, *sql.DB) string                            = dbtest.DeploymentSchema
)
