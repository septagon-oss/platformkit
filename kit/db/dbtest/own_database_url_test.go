package dbtest

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestOwnDatabaseURLNamesTheNewDatabaseAndKeepsTheCallersQuery(t *testing.T) {
	const raw = "postgres://u:p@localhost:5432/platformkit?application_name=caller+name&options=-c%20statement_timeout%3D7s&sslmode=disable"
	dsn, err := intoDatabase(raw, "platformkit_test_own")
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if config.Database != "platformkit_test_own" {
		t.Errorf("database = %q, want platformkit_test_own", config.Database)
	}
	if got := config.RuntimeParams["application_name"]; got != "caller+name" {
		t.Errorf("application_name = %q, want literal caller+name", got)
	}
	if got := config.RuntimeParams["options"]; got != "-c statement_timeout=7s" {
		t.Errorf("options = %q, want the caller's own", got)
	}
	if _, err := intoDatabase("postgres://localhost/platformkit?dbname=other", "platformkit_test_own"); err == nil {
		t.Error("a URL naming its database in the query was accepted")
	}
}
