// Package dbtest gives one test its own Postgres schema.
//
// It lives outside kit/db so that kit/db exports no DDL door: an application
// connection can only reach the database through db.Run or db.RunSystem, and
// the owner connection a test needs for CREATE TABLE is a plain *sql.DB opened
// here. Nothing in kit/ imports this package; only tests do.
//
// The schema travels in the connection URL, as options=-csearch_path=<schema>,
// so every connection a pool opens from it lands in the same place, and so does
// everything db.Migrate creates: the ledger and the tenancy helper functions are
// private to the test rather than shared through public.
package dbtest

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/migrations"
)

// URLs creates the schema and returns the two connection URLs that reach it:
// the first owns it and holds the DDL rights, the second is the unprivileged
// role row-level security binds. The schema is dropped when the test ends.
//
// It fails rather than skips when the environment names no database. A suite
// that quietly skips the isolation tests proves nothing.
func URLs(t *testing.T) (adminURL, appURL string) {
	t.Helper()
	ctx := t.Context()
	baseAdmin := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	baseApp := mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	name := schemaName(t.Name())
	schema := quote(name)
	role := quote(RoleOf(t, baseApp))

	admin := Open(t, baseAdmin)
	run := func(query string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, query); err != nil {
			t.Fatalf("dbtest: %s: %v", query, err)
		}
	}
	run("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
	run("CREATE SCHEMA " + schema)
	run("GRANT USAGE ON SCHEMA " + schema + " TO " + role)
	// Default privileges hand each table and sequence created in the schema to
	// the app role as it appears, the same way deploy/postgres/init.sql does for
	// the public schema.
	run("ALTER DEFAULT PRIVILEGES IN SCHEMA " + schema + " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + role)
	run("ALTER DEFAULT PRIVILEGES IN SCHEMA " + schema + " GRANT USAGE, SELECT ON SEQUENCES TO " + role)

	t.Cleanup(func() {
		cleaner, err := sql.Open("pgx", baseAdmin)
		if err != nil {
			return
		}
		defer cleaner.Close()
		_, _ = cleaner.ExecContext(context.WithoutCancel(ctx), "DROP SCHEMA "+schema+" CASCADE")
	})
	return withSchema(t, baseAdmin, name), withSchema(t, baseApp, name)
}

// Schema is URLs with migrations/ applied and both handles open: the owner as a
// plain *sql.DB, because DDL is all a test wants it for, and the application
// role as a *db.Conn, because db.Open is the only way to obtain one and it
// refuses any role row-level security would not bind.
func Schema(t *testing.T) (admin *sql.DB, app *db.Conn) {
	t.Helper()
	adminURL, appURL := URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.FS); err != nil {
		t.Fatalf("dbtest: migrate: %v", err)
	}
	admin = Open(t, adminURL)
	app, err := db.Open(t.Context(), appURL)
	if err != nil {
		t.Fatalf("dbtest: application connection: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	return admin, app
}

// Open is a database/sql pool on rawURL, closed when the test ends.
func Open(t *testing.T, rawURL string) *sql.DB {
	t.Helper()
	pool, err := sql.Open("pgx", rawURL)
	if err != nil {
		t.Fatalf("dbtest: open %q: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// RoleOf is the login role of a connection URL, so a test names the same role
// the app connects as instead of hard-coding it a second time.
func RoleOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("dbtest: %q is not a URL: %v", rawURL, err)
	}
	if u.User == nil || u.User.Username() == "" {
		t.Fatalf("dbtest: %q names no role", rawURL)
	}
	return u.User.Username()
}

// withSchema returns rawURL with the test schema as the only search_path. pgx
// forwards `options` to the startup packet, so it applies to every connection a
// pool opens from the URL, including the ones kit/app and db.Migrate open for
// themselves. A statement would bind one session instead, which is why the
// schema travels in the URL and not in a SET.
func withSchema(t *testing.T, rawURL, schema string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("dbtest: %q is not a URL: %v", rawURL, err)
	}
	q := u.Query()
	q.Set("options", "-csearch_path="+schema)
	u.RawQuery = q.Encode()
	return u.String()
}

func mustEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("dbtest: %s is unset; start the stack with `make up` and export the test URLs", name)
	}
	return v
}

// schemaName turns a test name into a Postgres identifier.
func schemaName(test string) string {
	var b strings.Builder
	b.WriteString("t_")
	for _, r := range strings.ToLower(test) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	name := b.String()
	if len(name) > 63 { // the Postgres identifier limit
		name = name[:63]
	}
	return name
}

func quote(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }
