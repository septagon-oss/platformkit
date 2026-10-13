package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestLockObservationKeepsItsOwnSchema(t *testing.T) {
	ownURL, _ := dbtest.URLs(t)
	own := dbtest.Open(t, ownURL)
	conn, err := own.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), cancellationNamespaceLock, cancellationCompositionKey); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.WithoutCancel(t.Context()), cancellationNamespaceUnlock, cancellationCompositionKey)
	t.Run("other", func(t *testing.T) {
		otherURL, _ := dbtest.URLs(t)
		other := dbtest.Open(t, otherURL)
		if dbtest.DeploymentSchema(t, own) == dbtest.DeploymentSchema(t, other) {
			t.Fatal("the fixture must use different namespaces")
		}
		var sessionName string
		scan(t, own, "SELECT current_setting('search_path')", &sessionName)
		otherConn, err := other.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer otherConn.Close()
		if _, err := otherConn.ExecContext(t.Context(), "SELECT set_config('application_name', $1, false)", sessionName); err != nil {
			t.Fatal(err)
		}
		if _, err := otherConn.ExecContext(t.Context(), cancellationNamespaceLock, cancellationCompositionKey); err != nil {
			t.Fatal(err)
		}
		defer otherConn.ExecContext(context.WithoutCancel(t.Context()), cancellationNamespaceUnlock, cancellationCompositionKey)
		var held int
		scan(t, own, cancellationKeyHolder, &held)
		if held != 1 {
			t.Fatalf("matching sessions hold %d observed locks, want only this schema's one", held)
		}
		var released bool
		if err := conn.QueryRowContext(t.Context(), cancellationNamespaceUnlock, cancellationCompositionKey).Scan(&released); err != nil {
			t.Fatal(err)
		}
		if !released {
			t.Fatal("this schema's lock was not held at release")
		}
		scan(t, own, cancellationKeyHolder, &held)
		if held != 0 {
			t.Fatalf("after release observed %d locks from the other schema, want zero", held)
		}
	})
}
