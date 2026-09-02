package db

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestSchema gives one test its own Postgres schema and the two connections the
// architecture is built on: admin owns the schema and runs DDL, app connects as
// the unprivileged role that row-level security binds. The schema is dropped
// when the test ends.
//
// It fails rather than skips when the URLs are unset. A suite that quietly
// skips the isolation tests proves nothing, which is exactly the state
// docs/adr/0003 was written to end.
func TestSchema(t *testing.T) (admin, app *Conn) {
	t.Helper()
	ctx := t.Context()
	adminURL := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	appURL := mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	schema := quote(schemaName(t.Name()))
	role := quote(roleOf(t, appURL))

	admin, err := openOwner(adminURL)
	if err != nil {
		t.Fatalf("db: test admin connection: %v", err)
	}
	pin(t, admin)
	run := func(c *Conn, query string) {
		t.Helper()
		if err := c.exec(ctx, query); err != nil {
			t.Fatalf("db: TestSchema: %v", err)
		}
	}
	run(admin, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
	run(admin, "CREATE SCHEMA "+schema)
	run(admin, "GRANT USAGE ON SCHEMA "+schema+" TO "+role)
	// Default privileges hand each table and sequence the test creates to the
	// app role as it appears, the same way deploy/postgres/init.sql does for
	// the public schema.
	run(admin, "ALTER DEFAULT PRIVILEGES IN SCHEMA "+schema+" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "+role)
	run(admin, "ALTER DEFAULT PRIVILEGES IN SCHEMA "+schema+" GRANT USAGE, SELECT ON SEQUENCES TO "+role)
	// public stays on the path: migrations/ installs the tenancy helper
	// functions there once for the whole database.
	run(admin, "SET search_path TO "+schema+", public")

	app, err = Open(ctx, appURL)
	if err != nil {
		_ = admin.Close()
		t.Fatalf("db: test app connection: %v", err)
	}
	pin(t, app)
	run(app, "SET search_path TO "+schema+", public")

	t.Cleanup(func() {
		done := context.WithoutCancel(ctx)
		_ = app.Close()
		_ = admin.exec(done, "DROP SCHEMA "+schema+" CASCADE")
		_ = admin.Close()
	})
	return admin, app
}

// pin holds a pool at one connection so that SET search_path, which is a
// session setting, applies to every statement the test makes afterwards.
func pin(t *testing.T, c *Conn) {
	t.Helper()
	sqlDB, err := c.db.DB()
	if err != nil {
		t.Fatalf("db: pool: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)
}

func mustEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("db: %s is unset; start the stack with `make up` and export the test URLs", name)
	}
	return v
}

// roleOf is the login role of a connection URL, so the grants above name the
// same role the app connects as instead of hard-coding it a second time.
func roleOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("db: PLATFORMKIT_TEST_DATABASE_URL is not a URL: %v", err)
	}
	if u.User == nil || u.User.Username() == "" {
		t.Fatalf("db: PLATFORMKIT_TEST_DATABASE_URL names no role")
	}
	return u.User.Username()
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
