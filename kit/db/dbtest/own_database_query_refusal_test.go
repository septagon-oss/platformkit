package dbtest

import (
	"strings"
	"testing"
)

// The fixture names the database in the URL's path. A query that names one too is a second answer
// to the same question, and pgconn reads `database` as readily as `dbname`, so a URL carrying
// either would run the package in the database its author wrote rather than the one the fixture
// made and dropped. Both spellings are refused; the case asks each of them.
func TestOwnDatabaseRefusesEitherQuerySpellingOfItsDatabase(t *testing.T) {
	for _, raw := range []string{
		"postgres://localhost/platformkit?dbname=shared",
		"postgres://localhost/platformkit?database=shared",
		"postgres://localhost/platformkit?sslmode=disable&database=shared",
	} {
		if _, err := intoDatabase(raw, "platformkit_test_own"); err == nil {
			t.Errorf("%s was accepted: the fixture would name one database while pgx read another", raw)
		}
	}
}

// What is not a database name is left exactly as it was written, including a pair whose value
// merely looks like one: the fixture rewrites the path and nothing else, because re-encoding a
// query changes what the server is told.
func TestOwnDatabaseKeepsAQueryThatOnlyLooksLikeADatabaseName(t *testing.T) {
	const raw = "postgres://u@localhost:5432/platformkit?database_url=shared&search_path=pkit%20test"
	dsn, err := intoDatabase(raw, "platformkit_test_own")
	if err != nil {
		t.Fatal(err)
	}
	rest := dsn[strings.Index(dsn, "?")+1:]
	if rest != "database_url=shared&search_path=pkit%20test" {
		t.Errorf("query = %q, want the caller's pairs undecoded and unreordered", rest)
	}
}
