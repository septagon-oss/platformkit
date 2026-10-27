package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The line about the effects an ending dropped is their only trace, and on an ending whose
// answer the database refused the caller is handed that same driver's error and nothing else.
// "committed=false rolled_back=false" says only that nothing was confirmed: what the server
// answered is the fact a reader needs to judge whether the rows were kept, so the line carries
// it. An ending the database answered cleanly has no refusal, and naming one would put a
// failure behind an ending that had none.

// droppedLine answers the one line out of a log capture that reports an ending which ended
// without the effects deferred to it, failing when the capture holds none or holds two.
func droppedLine(t *testing.T, out string) string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ended without the effects deferred to it") {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("the ending wrote %d line(s) about the effects it dropped, want exactly one: %s", len(lines), out)
	}
	return lines[0]
}

func TestTheLineAboutDroppedEffectsNamesTheAnswerTheDatabaseGave(t *testing.T) {
	admin, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "behind a session the server ended")
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { return nil })
	tx, err := p.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	endSession(t, admin, txSessionPID(t, tx))
	var refused error
	out := endingLogDebug(t, func() {
		refused = p.Rollback()
		if refused == nil {
			t.Fatal("Rollback answered nil for a session the server had ended: nothing was confirmed")
		}
	})
	// 57P01 is PostgreSQL's own code for a session the server ended — what this case did to the
	// transaction, and the answer the caller was handed. The line has to say it.
	line := droppedLine(t, out)
	for _, want := range []string{"error=", "57P01"} {
		if !strings.Contains(line, want) {
			t.Errorf("the line about the dropped effects does not name %s, the answer the caller was handed (%v): %s", want, refused, line)
		}
	}
}

func TestTheLineAboutDroppedEffectsOfAnAnsweredEndingNamesNoRefusal(t *testing.T) {
	_, app, acme := acmeThings(t)
	ctx, p, _ := lazy(t, app, acme)
	insertThrough(t, ctx, p, acme, "rolled back clean")
	mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { return nil })
	out := endingLogDebug(t, func() {
		if err := p.Rollback(); err != nil {
			t.Fatal(err)
		}
	})
	line := droppedLine(t, out)
	if !strings.Contains(line, "rolled_back=true") {
		t.Errorf("the line about the effect a clean ROLLBACK dropped does not say the ending was confirmed: %s", line)
	}
	if strings.Contains(line, "error=") {
		t.Errorf("the line about the effect a clean ROLLBACK dropped names a refusal the ending never had: %s", line)
	}
}
