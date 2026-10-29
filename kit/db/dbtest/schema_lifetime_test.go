package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestWithSchemaReleasesPoolsAndNamespace(t *testing.T) {
	observer := Open(t, mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL"))
	failure := errors.New("callback refused")
	for _, end := range []string{"success", "error", "panic", "cancel"} {
		t.Run(end, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			var schema string
			var owner *sql.DB
			var conn *db.Conn
			calls := 0
			var err error
			func() {
				defer func() {
					p := recover()
					if end == "panic" && p != failure || end != "panic" && p != nil {
						t.Errorf("panic = %v for %s", p, end)
					}
				}()
				err = WithSchema(ctx, t.Name(), func(ctx context.Context, admin *sql.DB, app *db.Conn) error {
					calls++
					owner, conn = admin, app
					if err := admin.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
						return err
					}
					if admin.Stats().MaxOpenConnections != 4 || app.Stats().MaxOpenConnections != 16 {
						return errors.New("fixture lost its pool bounds")
					}
					if err := db.RunSystem(ctx, app, SystemToken(), func(_ context.Context, tx db.Tx[db.System]) error {
						var unrestricted bool
						if err := tx.DB().Raw("SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user").Row().Scan(&unrestricted); err != nil {
							return err
						}
						if unrestricted {
							return errors.New("application bypasses RLS")
						}
						return tx.DB().Exec("SELECT count(*) FROM platformkit_outbox").Error
					}); err != nil {
						return err
					}
					switch end {
					case "error":
						return failure
					case "panic":
						panic(failure)
					case "cancel":
						cancel()
						return ctx.Err()
					}
					return nil
				})
			}()
			want := map[string]error{"error": failure, "cancel": context.Canceled}[end]
			if !errors.Is(err, want) || calls != 1 {
				t.Fatalf("calls=%d err=%v, want %v", calls, err, want)
			}
			assertSchemaGone(t, observer, schema)
			if err := owner.PingContext(t.Context()); err == nil {
				t.Error("owner pool still open")
			}
			if err := db.RunSystem(t.Context(), conn, SystemToken(), func(context.Context, db.Tx[db.System]) error { return nil }); err == nil {
				t.Error("application pool still open")
			}
		})
	}
}

func assertSchemaGone(t *testing.T, observer *sql.DB, schema string) {
	t.Helper()
	var exists bool
	if err := observer.QueryRowContext(t.Context(), "SELECT EXISTS (SELECT FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists || schema == "" {
		t.Fatalf("schema %q remains or was never observed", schema)
	}
}

func TestWithSchemaRemovesFailedMigrationNamespace(t *testing.T) {
	observer := Open(t, mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL"))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	err := WithSchema(ctx, "broken migration", func(context.Context, *sql.DB, *db.Conn) error {
		t.Error("failed setup invoked callback")
		return nil
	}, db.MigrationSource{Owner: "broken", Files: fstest.MapFS{
		"1_broken.up.sql": {Data: []byte("THIS IS INVALID SQL;")},
	}})
	if err == nil {
		t.Fatal("invalid migration succeeded")
	}
	var left int
	if err := observer.QueryRowContext(t.Context(), "SELECT count(*) FROM pg_namespace WHERE starts_with(nspname, $1)", "t_"+run+"_"+sanitize("broken migration")+"_").Scan(&left); err != nil || left != 0 {
		t.Fatalf("partial namespaces=%d: %v", left, err)
	}
}

func TestWithSchemaConcurrentLabelsStayIsolated(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	ready, release := make(chan string, 2), make(chan struct{})
	var workers sync.WaitGroup
	defer func() { cancel(); close(release); workers.Wait() }()
	finished := make(chan error, 2)
	label := strings.Repeat("same long label", 10)
	for range 2 {
		workers.Go(func() {
			finished <- WithSchema(ctx, label, func(ctx context.Context, owner *sql.DB, _ *db.Conn) error {
				var schema string
				if err := owner.QueryRowContext(ctx, "SELECT current_schema()").Scan(&schema); err != nil {
					return err
				}
				ready <- schema
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				_, err := owner.ExecContext(ctx, "CREATE TABLE private_marker (id integer)")
				return err
			})
		})
	}
	names := make([]string, 0, 2)
	for range 2 {
		select {
		case name := <-ready:
			names = append(names, name)
		case err := <-finished:
			t.Fatalf("setup finished before both callbacks: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if names[0] == names[1] || len(names[0]) > identifierLimit || len(names[1]) > identifierLimit {
		t.Fatalf("schema names collide or exceed identifier limit: %v", names)
	}
	// Let one invocation drop its schema before the other resumes its own work.
	for range 2 {
		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
	observer := Open(t, mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL"))
	for _, name := range names {
		assertSchemaGone(t, observer, name)
	}
}

func TestWithSchemaRefusesInvalidInputsAndMissingURLs(t *testing.T) {
	callback := func(context.Context, *sql.DB, *db.Conn) error {
		t.Error("invalid setup invoked callback")
		return nil
	}
	for _, err := range []error{
		WithSchema(nil, "label", callback),
		WithSchema(t.Context(), " ", callback),
		WithSchema(t.Context(), "label", nil),
	} {
		if err == nil {
			t.Error("invalid argument accepted")
		}
	}
	for _, name := range []string{"PLATFORMKIT_TEST_ADMIN_URL", "PLATFORMKIT_TEST_DATABASE_URL"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "")
			if err := WithSchema(t.Context(), "label", callback); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("missing %s: %v", name, err)
			}
		})
	}
}
