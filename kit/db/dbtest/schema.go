package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/migrations"
)

var schemaInvocation atomic.Uint64

// WithSchema calls run once with an isolated, migrated schema's owner and
// RLS-bound application pools, then closes them and drops the schema. Handles
// must not escape run. Like Schema, it requires the two test URL variables and
// applies kernel migrations followed by extra. A nil context/callback or blank
// diagnostic name is refused. Setup, callback and cleanup errors are joined;
// cleanup has a separate minute even after cancellation and also runs on panic.
func WithSchema(ctx context.Context, name string, run func(context.Context, *sql.DB, *db.Conn) error, extra ...db.MigrationSource) (err error) {
	if ctx == nil || strings.TrimSpace(name) == "" || run == nil {
		return errors.New("dbtest: WithSchema requires a context, diagnostic name and callback")
	}
	f, err := openSchema(ctx, invocationName(name), nil, extra...)
	defer func() {
		panicked := recover()
		cleanupErr := f.close(ctx)
		if panicked != nil {
			if cleanupErr != nil {
				fmt.Fprintln(os.Stderr, "dbtest: cleanup after panic:", cleanupErr)
			}
			panic(panicked)
		}
		err = errors.Join(err, cleanupErr)
	}()
	if err != nil {
		return err
	}
	return run(ctx, f.admin, f.app)
}

func invocationName(label string) string {
	prefix := "t_" + run + "_"
	suffix := "_" + strconv.FormatUint(schemaInvocation.Add(1), 36)
	label = sanitize(label)
	return prefix + label[:min(len(label), identifierLimit-len(prefix)-len(suffix))] + suffix
}

type namespace struct {
	name, baseAdmin, adminURL, appURL string
}

// createNamespace shares URL validation and grants with every schema fixture.
// A nonnil result owns cleanup even when setup failed partway through.
func createNamespace(ctx context.Context, name string) (ns *namespace, err error) {
	baseAdmin, err := requiredURL("PLATFORMKIT_TEST_ADMIN_URL")
	if err != nil {
		return nil, err
	}
	baseApp, err := requiredURL("PLATFORMKIT_TEST_DATABASE_URL")
	if err != nil {
		return nil, err
	}
	role, err := loginRole(baseApp)
	if err != nil {
		return nil, err
	}
	adminURL, err := schemaURL(baseAdmin, name)
	if err != nil {
		return nil, err
	}
	appURL, err := schemaURL(baseApp, name)
	if err != nil {
		return nil, err
	}
	owner, err := openOwner(baseAdmin)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, owner.Close()) }()
	ns = &namespace{name: name, baseAdmin: baseAdmin, adminURL: adminURL, appURL: appURL}
	for _, query := range namespaceStatements(name, role) {
		if _, err := owner.ExecContext(ctx, query); err != nil {
			return ns, fmt.Errorf("dbtest: create namespace: %w", err)
		}
	}
	return ns, nil
}

func requiredURL(name string) (string, error) {
	if value := os.Getenv(name); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("dbtest: %s is unset; start the stack with `make up` and export the test URLs", name)
}

func (ns *namespace) close(ctx context.Context) (err error) {
	if ns == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	owner, err := openOwner(ns.baseAdmin)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, owner.Close()) }()
	if _, err := owner.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quote(ns.name)+" CASCADE"); err != nil {
		return fmt.Errorf("dbtest: drop schema %s: %w", ns.name, err)
	}
	return nil
}

type schemaFixture struct {
	ns    *namespace
	admin *sql.DB
	app   *db.Conn
}

func openSchema(ctx context.Context, name string, logf func(string, ...any), extra ...db.MigrationSource) (*schemaFixture, error) {
	f := &schemaFixture{}
	var err error
	f.ns, err = createNamespace(ctx, name)
	if err != nil {
		return f, err
	}
	sources := append([]db.MigrationSource{migrations.Source}, extra...)
	if err := migrateWithLog(ctx, f.ns.adminURL, logf, sources...); err != nil {
		return f, fmt.Errorf("dbtest: migrate: %w", err)
	}
	f.admin, err = openOwner(f.ns.adminURL)
	if err != nil {
		return f, err
	}
	f.app, err = db.Open(ctx, f.ns.appURL)
	if err != nil {
		return f, fmt.Errorf("dbtest: application connection: %w", err)
	}
	return f, nil
}

func (f *schemaFixture) close(ctx context.Context) error {
	var err error
	if f.app != nil {
		err = f.app.Close()
	}
	if f.admin != nil {
		err = errors.Join(err, f.admin.Close())
	}
	return errors.Join(err, f.ns.close(ctx))
}
