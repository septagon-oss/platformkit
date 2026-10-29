package db_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// An ending under an owner that had already ended is a confirmed rollback: database/sql had
// discarded the transaction before our own ROLLBACK was refused as already done, so the work
// waiting for a commit is dropped on a known outcome and the line about it is debug. It is the
// one dropped-effects line that is debug and still carries an error, and the error it carries is
// the owner's cause — the same answer the caller is handed and the only trace of why the ending
// happened at all. Both facts are asserted on that line rather than anywhere in the capture, so
// an unrelated warning elsewhere in the process answers neither of them.

func TestTheLineAboutDroppedEffectsOfAnEndedOwnerNamesTheCauseItsCallerWasHanded(t *testing.T) {
	admin, app, acme := acmeThings(t)
	ctx, p, cancel := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "the owner hung up")
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { return nil })
	cancel()
	var refused error
	out := endingLogDebug(t, func() {
		refused = p.Rollback()
	})
	// What decides that this ending was the owner's and not some other is the caller's own answer
	// and the row the database discarded, so the case stands whatever the line says.
	if !errors.Is(refused, context.Canceled) {
		t.Fatalf("Rollback answered %v, want the owner's cause: the case never reached an ended owner", refused)
	}
	if n := committed(t, admin); n != 0 {
		t.Fatalf("%d row(s) of the ended transaction are committed, want none: an ending the database confirmed", n)
	}
	line := droppedLine(t, out)
	for _, want := range []string{"level=DEBUG", "committed=false", "rolled_back=true", "error=", refused.Error()} {
		if !strings.Contains(line, want) {
			t.Errorf("the line about the effects an ended owner dropped does not name %s: %s", want, line)
		}
	}
}
