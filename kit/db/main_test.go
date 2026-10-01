package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Migration locks belong to a database, not a schema. Give this package its
// own database so its deliberate lock holds and acquisition deadlines measure
// its own sessions, independently of other packages migrating at the same time.
// Individual tests still use dbtest's schemas, grants and application role.
func TestMain(m *testing.M) {
	os.Exit(runInTestDatabase(m))
}

func runInTestDatabase(m *testing.M) (code int) {
	names := []string{"PLATFORMKIT_TEST_ADMIN_URL", "PLATFORMKIT_TEST_DATABASE_URL"}
	urls := make([]*url.URL, len(names))
	for i, name := range names {
		raw := os.Getenv(name)
		if raw == "" {
			// Pure tests can run without services. Database tests still fail at
			// dbtest's existing required-environment check; none are skipped.
			return m.Run()
		}
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
			fmt.Fprintf(os.Stderr, "kit/db fixture: %s must be a PostgreSQL URL\n", name)
			return 1
		}
		urls[i] = parsed
	}
	admin, err := sql.Open("pgx", urls[0].String())
	if err != nil {
		fmt.Fprintln(os.Stderr, "kit/db fixture: could not open the owner connection")
		return 1
	}
	defer admin.Close()

	// The name is generated here, never supplied by a caller. Like the browser
	// fixture, this requires the development owner to have CREATE DATABASE.
	database := "platformkit_dbtest_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	setup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(setup, "CREATE DATABASE "+database); err != nil {
		fmt.Fprintf(os.Stderr, "kit/db fixture: create database: %v\n", err)
		return 1
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+database+" WITH (FORCE)"); err != nil {
			fmt.Fprintf(os.Stderr, "kit/db fixture: drop database: %v\n", err)
			code = 1
		}
	}()

	for i, name := range names {
		urls[i].Path = "/" + database
		urls[i].RawPath = ""
		// A database query parameter takes precedence over the URL path in
		// pgx. Keep both forms bound to the database this process will remove.
		query := urls[i].Query()
		query.Del("dbname")
		query.Del("database")
		urls[i].RawQuery = query.Encode()
		if err := os.Setenv(name, urls[i].String()); err != nil {
			fmt.Fprintf(os.Stderr, "kit/db fixture: set %s: %v\n", name, err)
			return 1
		}
	}
	return m.Run()
}
