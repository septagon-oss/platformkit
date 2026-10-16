package dbtest_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestOwnerPoolsBoundConnectionsAndRetainOneIdle(t *testing.T) {
	for _, name := range []string{"Open", "OpenFor"} {
		t.Run(name, func(t *testing.T) {
			rawURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
			if rawURL == "" {
				t.Fatal("PLATFORMKIT_TEST_ADMIN_URL is required")
			}
			var pool *sql.DB
			if name == "Open" {
				pool = dbtest.Open(t, rawURL)
			} else {
				pool = dbtest.OpenFor(t, rawURL)
			}
			if got := pool.Stats().MaxOpenConnections; got != 4 {
				t.Fatalf("maximum open connections before first use = %d, want 4", got)
			}
			var held []*sql.Conn
			defer func() {
				for _, conn := range held {
					_ = conn.Close()
				}
			}()
			for range 4 {
				conn, err := pool.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				held = append(held, conn)
			}
			if got := pool.Stats().InUse; got != 4 {
				t.Fatalf("simultaneous connections = %d, want 4", got)
			}
			for _, conn := range held {
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
			}
			held = nil
			if got := pool.Stats(); got.Idle != 1 || got.OpenConnections != 1 {
				t.Fatalf("after release: %+v, want one idle connection", got)
			}
		})
	}
}

func TestSchemaBoundsOwnerAndPreservesApplicationPool(t *testing.T) {
	owner, app := dbtest.Schema(t)
	if got := owner.Stats().MaxOpenConnections; got != 4 {
		t.Errorf("owner maximum = %d, want 4", got)
	}
	if got := app.Stats().MaxOpenConnections; got != 16 {
		t.Errorf("application maximum = %d, want 16", got)
	}
}
