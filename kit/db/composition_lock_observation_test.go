package db_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestCancellationObservationSeesTheHeldNamespaceLock(t *testing.T) {
	url, _ := dbtest.URLs(t)
	admin := dbtest.Open(t, url)
	conn, err := admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), cancellationNamespaceLock, cancellationCompositionKey); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.WithoutCancel(t.Context()), cancellationNamespaceUnlock, cancellationCompositionKey)
	var held int
	scan(t, admin, cancellationKeyHolder, &held)
	if held != 1 {
		t.Fatalf("holder observation = %d while this schema's lock is held, want 1", held)
	}
	var released bool
	if err := conn.QueryRowContext(t.Context(), cancellationNamespaceUnlock, cancellationCompositionKey).Scan(&released); err != nil {
		t.Fatal(err)
	}
	if !released {
		t.Fatal("namespace lock was not held at release")
	}
	scan(t, admin, cancellationKeyHolder, &held)
	if held != 0 {
		t.Fatalf("holder observation = %d after release, want 0", held)
	}
}
