package dbtest_test

// One database carries every package of this suite. A concurrent index build waits
// for the transactions that were open when its snapshot was taken, whatever table
// those transactions touched, so a fixture that migrates a source holding such a file
// meets another package's open write as the ordinary case rather than the rare one.
// The runner's own five-second lock budget is what a boot wants and not what a fixture
// wants: a boot that cannot get a lock should stop and say so, and a fixture that stops
// reports a migration failure for a schema it was never given. This case holds a write
// open past that five seconds and asks for the schema anyway.

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestSchemaMigratesWhileAnotherCaseHoldsAWriteOpen(t *testing.T) {
	// The stranger: its own schema, its own table, and a write held eight seconds,
	// which is past the five the runner gives a lock by default. Nothing here shares a
	// table with the fixture below; that is the point of the shape.
	strangerURL, _ := dbtest.URLs(t)
	stranger := dbtest.Open(t, strangerURL)
	hold := context.WithoutCancel(t.Context())
	if _, err := stranger.ExecContext(hold,
		`CREATE TABLE elsewhere (id integer)`); err != nil {
		t.Fatalf("the stranger's table: %v", err)
	}
	conn, err := stranger.Conn(hold)
	if err != nil {
		t.Fatalf("the stranger's connection: %v", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(hold, nil)
	if err != nil {
		t.Fatalf("the stranger's transaction: %v", err)
	}
	if _, err := tx.ExecContext(hold, `INSERT INTO elsewhere VALUES (1)`); err != nil {
		t.Fatalf("the stranger's write: %v", err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(8 * time.Second)
		_ = tx.Rollback()
	}()
	t.Cleanup(func() { <-released })

	source := db.MigrationSource{Owner: "contended", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(
			`CREATE TABLE probe (id integer PRIMARY KEY, sent boolean NOT NULL DEFAULT false)`)},
		"000002_sent_once.up.sql": {Data: []byte(`-- pkit: autocommit=true
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS probe_sent_once ON probe (id) WHERE sent;`)},
	}}

	started := time.Now()
	dbtest.Schema(t, source) // it fails the test with the migration's own words if it refuses
	elapsed := time.Since(started)

	// The wait, not just the success: a fixture that came back inside the five seconds
	// it is asking to be given met no open transaction at all, and this case would be
	// proving the shape of the schedule rather than the patience of the fixture.
	if elapsed < 5*time.Second {
		t.Errorf("the fixture migrated in %s, inside the budget a stranger was holding: "+
			"the build never waited, so nothing here was measured", elapsed)
	}
	<-released
}
