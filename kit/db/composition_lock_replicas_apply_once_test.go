package db_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTwoReplicasOfOneNamespaceApplyAFileOnce pins the other half of the namespaced
// composition key: two runs of one installation still queue on its key, and once it is
// given back the file is applied exactly once, not once per replica.
func TestTwoReplicasOfOneNamespaceApplyAFileOnce(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	own := schemaOf(t, migrateURL)
	holder := dbtest.Open(t, migrateURL)
	conn, err := holder.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), namespaceLockSQL, namespaceLockKey, own); err != nil {
		t.Fatalf("take the composition lock of %s: %v", own, err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.WithoutCancel(t.Context()), namespaceUnlockSQL, namespaceLockKey, own)
	}()
	source := db.MigrationSource{Owner: "replicas", Files: fstest.MapFS{
		"000001_head.up.sql": {Data: []byte("CREATE TABLE replicas_head (id bigint PRIMARY KEY)")},
	}}
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- db.MigrateWith(context.WithoutCancel(t.Context()), migrateURL, db.MigrationBudget{}, source)
		}()
	}
	if !waitsOnOwnCompositionKey(t, holder, own) {
		t.Fatalf("no replica of %s queued on its held composition key", own)
	}
	if _, err := conn.ExecContext(t.Context(), namespaceUnlockSQL, namespaceLockKey, own); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("a replica returned %v after the key was given back", err)
		}
	}
	if n := countRows(t, holder, "SELECT count(*) FROM schema_migrations WHERE owner = 'replicas'"); n != 1 {
		t.Fatalf("two replicas left %d history rows, want 1", n)
	}
}
