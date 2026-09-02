package db

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestSchemaURLs gives one test its own Postgres schema and the two connection
// URLs that reach it: the first owns the schema and holds the DDL rights, the
// second is the unprivileged role row-level security binds. The schema is
// dropped when the test ends.
//
// The schema travels in the URL, as the search_path connection parameter, so
// every connection a pool opens from it lands in the same place. Setting it
// with a statement instead would bind one session, which is why code that opens
// its own pool — kit/app, and kit/db.Migrate — needs the URL rather than a
// *Conn.
//
// It fails rather than skips when the URLs are unset. A suite that quietly
// skips the isolation tests proves nothing.
func TestSchemaURLs(t *testing.T) (adminURL, appURL string) {
	t.Helper()
	ctx := t.Context()
	baseAdmin := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	baseApp := mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	name := schemaName(t.Name())
	schema := quote(name)
	role := quote(roleOf(t, baseApp))

	admin, err := openOwner(baseAdmin)
	if err != nil {
		t.Fatalf("db: test admin connection: %v", err)
	}
	defer admin.Close()
	run := func(query string) {
		t.Helper()
		if err := admin.exec(ctx, query); err != nil {
			t.Fatalf("db: TestSchemaURLs: %v", err)
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
		done := context.WithoutCancel(ctx)
		cleaner, err := openOwner(baseAdmin)
		if err != nil {
			return
		}
		defer cleaner.Close()
		_ = cleaner.exec(done, "DROP SCHEMA "+schema+" CASCADE")
	})
	// public stays on the path: migrations/ installs the tenancy helper
	// functions there once for the whole database.
	return withSearchPath(t, baseAdmin, name), withSearchPath(t, baseApp, name)
}

// TestSchema is TestSchemaURLs with both connections already open.
func TestSchema(t *testing.T) (admin, app *Conn) {
	t.Helper()
	adminURL, appURL := TestSchemaURLs(t)
	admin, err := openOwner(adminURL)
	if err != nil {
		t.Fatalf("db: test admin connection: %v", err)
	}
	app, err = Open(t.Context(), appURL)
	if err != nil {
		_ = admin.Close()
		t.Fatalf("db: test app connection: %v", err)
	}
	t.Cleanup(func() {
		_ = app.Close()
		_ = admin.Close()
	})
	return admin, app
}

// withSearchPath returns rawURL with search_path set to the test schema. pgx
// forwards any query parameter it does not recognise as a connection setting,
// so this arrives in the startup message of every connection in the pool.
func withSearchPath(t *testing.T, rawURL, schema string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("db: %q is not a URL: %v", rawURL, err)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	return u.String()
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
