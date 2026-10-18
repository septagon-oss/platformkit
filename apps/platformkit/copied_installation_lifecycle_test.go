package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestConcurrentCopiesKeepTheirWritesAndLeaveNoDatabases(t *testing.T) {
	owner := dbtest.Open(t, os.Getenv("PLATFORMKIT_TEST_ADMIN_URL"))
	names := make([]string, 2)
	ready := []chan struct{}{make(chan struct{}), make(chan struct{})}
	t.Run("copies", func(t *testing.T) {
		for index, value := range []string{"first copy", "second copy"} {
			t.Run(value, func(t *testing.T) {
				t.Parallel()
				announce := sync.OnceFunc(func() { close(ready[index]) })
				defer announce()
				adminURL, appURL := dbtest.Clone(t)
				admin := dbtest.Open(t, adminURL)
				names[index] = current(t, admin, "SELECT current_database()")
				if _, err := admin.ExecContext(t.Context(), "CREATE TABLE copy_identity (value text NOT NULL)"); err != nil {
					t.Fatal(err)
				}
				app := dbtest.Open(t, appURL)
				if _, err := app.ExecContext(t.Context(), "INSERT INTO copy_identity VALUES ($1)", value); err != nil {
					t.Fatal(err)
				}
				announce()
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				select {
				case <-ready[1-index]:
				case <-ctx.Done():
					t.Fatal("the other copy did not finish its write:", ctx.Err())
				}
				if got := rowsIn(t, app, "copy_identity"); got != 1 {
					t.Errorf("copy holds %d rows, want its one write", got)
				}
				if got := current(t, app, "SELECT value FROM copy_identity"); got != value {
					t.Errorf("copy holds %q, want %q", got, value)
				}
				if got := current(t, app, "SELECT current_database()"); got != names[index] {
					t.Errorf("application connects to %q, owner connects to %q", got, names[index])
				}
			})
		}
	})
	if names[0] == "" || names[1] == "" {
		t.Fatal("both copies must reach their own database before checking cleanup")
	}
	if names[0] == names[1] {
		t.Errorf("two concurrent copies reached the same database %q", names[0])
	}
	for _, name := range names {
		var remains bool
		if err := owner.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", name).Scan(&remains); err != nil {
			t.Fatal(err)
		}
		if remains {
			t.Errorf("copy database %q remains after its case ended", name)
		}
	}
}
