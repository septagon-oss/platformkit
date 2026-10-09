package dbtest

import "testing"

func TestOwnDatabaseRefusesAmbiguousQueriesWithoutReturningAConnectionURL(t *testing.T) {
	for _, scheme := range []string{"postgres", "postgresql"} {
		for _, query := range []string{
			"database=",
			"dbname=",
			"database=first&database=second",
			"dbname=first&database=second",
			"application_name=caller+name&database=other&options=-c%20statement_timeout%3D7s",
		} {
			t.Run(scheme+"/"+query, func(t *testing.T) {
				dsn, err := intoDatabase(scheme+"://localhost/original?"+query, "platformkit_test_own")
				if err == nil {
					t.Fatal("ambiguous database query was accepted")
				}
				if dsn != "" {
					t.Errorf("refusal returned a usable connection URL: %q", dsn)
				}
			})
		}
	}
}
