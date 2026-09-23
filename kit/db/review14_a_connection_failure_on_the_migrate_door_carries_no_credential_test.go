package db_test

// review14_a_connection_failure_on_the_migrate_door_names_the_host_and_not_the_password_test.go
// pins the one thing the two new doors (MigrateWith and BackfillWith, which
// `platformkit migrate`, `app.Drain` and `jobs.BackfillMigrations` all reach) share with
// every other error path in the package: the sentences they return are the ones an
// operator's terminal, a worker log and `scripts/rehearse_migrations.sh`'s output all
// carry into a ticket. A `migrate_url` carries a password, and both doors take it as a
// plain argument and wrap whatever the driver says (`db: backfill: open: %w`,
// `db: migrate: %w`), so the promise worth pinning is that a run that could not connect
// reports *where* it could not connect without reporting what it paid to get in.
//
// Nothing else in the package asks the question of the new doors: the pool constructors
// refuse a superuser or BYPASSRLS role (`pool_test.go`) and the revoke cases read the
// grants back, but no case puts a credential through a failing `Migrate` or `Backfill`.

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestAConnectionFailureOnEitherMigrationDoorCarriesNoCredential(t *testing.T) {
	// Port 1 on localhost: nothing listens, so the run fails at connect on both
	// doors, which is the only failure both of them share that needs no database.
	const secret = "hunter2-not-a-real-password"
	url := "postgres://platformkit_owner:" + secret + "@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=2"
	source := db.MigrationSource{Owner: "unreachable", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY)")},
	}}

	for _, door := range []struct {
		name string
		run  func() error
	}{
		{"Migrate", func() error { return db.Migrate(t.Context(), url, source) }},
		{"Backfill", func() error { return db.Backfill(t.Context(), url, source) }},
	} {
		t.Run(door.name, func(t *testing.T) {
			err := door.run()
			if err == nil {
				t.Fatal("the run reported success against a port nothing listens on")
			}
			if !strings.Contains(err.Error(), "127.0.0.1") {
				t.Errorf("the failure says nothing about the host it could not reach, so an operator cannot tell which door is misconfigured: %v", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("the failure carries the password out of the connection string, and it will be logged wherever this run is logged: %v", err)
			}
		})
	}
}
