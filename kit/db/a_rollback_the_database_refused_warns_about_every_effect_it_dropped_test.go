package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The line about the effects an ending dropped is written at the level the database's own
// answer decides, and the two unknown endings have to read alike. A COMMIT that returned an
// error has its case; this is the other one. A ROLLBACK the server refused leaves the rows
// either kept or discarded with nothing to say which, so neither queue ran, both were dropped,
// and the caller is handed only the driver's own error: the log line is the whole trace, and a
// confirmed ending's debug level would bury it. effects counts both queues here, where the
// commit half counts one.

func TestARollbackTheDatabaseRefusedWarnsAboutEveryEffectItDropped(t *testing.T) {
	admin, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "behind a session the server ended")
	var ran int
	mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran++; return nil })
	tx, err := p.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	endSession(t, admin, txSessionPID(t, tx))
	out := endingLogDebug(t, func() {
		if err := p.Rollback(); err == nil {
			t.Fatal("Rollback answered nil for a session the server had ended: nothing was confirmed")
		}
	})
	const message = "ended without the effects deferred to it"
	if lines := strings.Count(out, message); lines != 1 {
		t.Fatalf("a refused ROLLBACK that dropped both queues wrote %d line(s) about it, want 1: %s", lines, out)
	}
	for _, want := range []string{"level=WARN", "effects=2", "committed=false", "rolled_back=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("the line about the effects a refused ROLLBACK dropped does not say %s: %s", want, out)
		}
	}
	if ran != 0 {
		t.Errorf("%d effects ran on a ROLLBACK the database refused, want none of them", ran)
	}
}
