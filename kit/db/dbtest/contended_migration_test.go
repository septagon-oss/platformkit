package dbtest

// contended_migration_test.go pins what the fixture does with a migration that was
// refused rather than one that failed. db.Migrate offers the re-run — "nothing this run
// had not already applied was applied, and it may be run again" — and an installation
// hands that decision to the operator; a fixture has nobody to hand it to, so pastContention
// takes the offer itself. kit/db's own TestTheLockBudgetStillRefusesAFileThatMustQueueBehindAWriter
// pins the error this waits on: the file that could not take the table lock within its
// budget answers db.ErrContended and applies none of itself.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
)

// refused is the CI answer, in the shape the runner wraps it: the file it was working on,
// the two budgets it was running under, and the wait that ran out underneath.
func refused(cause string) error {
	return fmt.Errorf("%s: %w (lock_timeout 5s, statement_timeout 0): ERROR: canceling "+
		"statement due to lock timeout (SQLSTATE 55P03)", cause, db.ErrContended)
}

func TestAContendedMigrationIsRunAgain(t *testing.T) {
	var tries int
	err := pastContention(t.Context(), migrateTries, time.Millisecond, func(ctx context.Context) error {
		tries++
		if tries < 3 {
			return refused("platformkit/000003_handled.up.sql")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the run that applied the files gave back %v", err)
	}
	if tries != 3 {
		t.Errorf("the schema was migrated %d times: two refusals, then the run that applied them", tries)
	}
}

func TestAMigrationThatFailedIsNotRunAgain(t *testing.T) {
	broke := errors.New("db: migrate: platformkit/000009_actor.up.sql: ERROR: syntax error")
	var tries int
	err := pastContention(t.Context(), migrateTries, time.Millisecond, func(ctx context.Context) error {
		tries++
		return broke
	})
	if !errors.Is(err, broke) || tries != 1 {
		t.Errorf("a migration that failed was answered with %v after %d attempts; only the lock "+
			"refusal is worth another try, and re-running a broken file hides it behind a delay", err, tries)
	}
}

func TestARefusalThatOutlastsTheTriesIsReportedAsTheRefusalItIs(t *testing.T) {
	var tries int
	err := pastContention(t.Context(), migrateTries, time.Millisecond, func(context.Context) error {
		tries++
		return refused("platformkit/000003_handled.up.sql")
	})
	if !errors.Is(err, db.ErrContended) {
		t.Errorf("every attempt refused and the fixture answered %v: the message a person reads "+
			"has to be the runner's own, budgets and all", err)
	}
	if tries != migrateTries {
		t.Errorf("the run was taken %d times, want the %d the fixture offers", tries, migrateTries)
	}
}

func TestAContendedMigrationStopsWhenTheTestEnds(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	var tries int
	err := pastContention(ctx, migrateTries, time.Hour, func(_ context.Context) error {
		tries++
		stop() // the wait after this attempt is the one the cancellation catches
		return refused("platformkit/000021_limits.up.sql")
	})
	if !errors.Is(err, db.ErrContended) {
		t.Errorf("the test ended mid-wait and the fixture answered %v, want the refusal it holds", err)
	}
	if tries != 1 {
		t.Errorf("the wait the test ended did not stop the fixture: %d attempts ran", tries)
	}
}

// TestAStoreThatNeverAnswersIsRefusedAtOnce goes through the door
// Schema uses, with a URL no server answers: the refusal there is a connection that never
// opened, which is not the offer to run again, and the fixture must say so at once.
func TestAStoreThatNeverAnswersIsRefusedAtOnce(t *testing.T) {
	start := time.Now()
	err := migrate(t, t.Context(), "postgres://nobody@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=1")
	if err == nil || errors.Is(err, db.ErrContended) {
		t.Fatalf("an unreachable store answered %v, want the connection's own refusal", err)
	}
	if spent := time.Since(start); spent >= migratePause/2 {
		t.Errorf("a migration nothing refused waited %s for it: only a refusal that offered a re-run "+
			"is waited on, and one wait of this fixture's own pause would have cost %s", spent, migratePause)
	}
}
