package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestLockObservationExcludesOtherSchemas(t *testing.T) {
	ownURL, _ := dbtest.URLs(t)
	own := dbtest.Open(t, ownURL)
	t.Run("other", func(t *testing.T) {
		otherURL, _ := dbtest.URLs(t)
		other := dbtest.Open(t, otherURL)
		if dbtest.DeploymentSchema(t, own) == dbtest.DeploymentSchema(t, other) {
			t.Fatal("the fixture must use different namespaces")
		}
		var sessionName string
		scan(t, own, "SELECT current_setting('search_path')", &sessionName)
		conn, err := other.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(t.Context(), "SELECT set_config('application_name', $1, false)", sessionName); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(t.Context(), cancellationNamespaceLock, cancellationCompositionKey); err != nil {
			t.Fatal(err)
		}
		defer conn.ExecContext(context.WithoutCancel(t.Context()), cancellationNamespaceUnlock, cancellationCompositionKey)
		var held int
		// This session uses the observer's name, so only the namespace can exclude it.
		var actual int
		const ownLocks = `SELECT count(*) FROM pg_locks
		WHERE pid = pg_backend_pid() AND locktype = 'advisory'
		AND classid = $1::oid AND objid = to_regnamespace(current_schema())::oid AND granted`
		if err := conn.QueryRowContext(t.Context(), ownLocks, cancellationCompositionKey).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		if actual != 1 {
			t.Fatalf("other namespace holds %d matching locks, want 1", actual)
		}
		scan(t, own, cancellationKeyHolder, &held)
		if held != 0 {
			t.Fatalf("holder observation counts %d locks from another namespace, want 0", held)
		}
	})
}
