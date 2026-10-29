package db_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A warning about the deferred work an ending dropped is worth having because that work is
// memory and its transaction is over: nothing else would say it ever existed. Both halves
// of the promise decide what an operator reads. A line written for an ending that dropped
// nothing puts a warning behind every ordinary commit, and a count that leaves out one of
// the two queues understates the one ending an operator most needs to read about — the
// COMMIT whose answer never came, which drops both.

// droppedEffectsLog runs fn with the default logger writing to a buffer and returns the
// lines it wrote about an ending that ended without the effects deferred to it.
func droppedEffectsLog(t *testing.T, fn func()) string {
	t.Helper()
	var logs strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	fn()
	var lines []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "ended without the effects deferred to it") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// Each ending runs the queue that waited for it and drops the other one's. When nothing
// waited, or only the work it ran waited, it wrote the line about it, the ordinary ending
// would warn once per commit and the warning would stop being about anything.
func TestAnEndingThatDroppedNothingWritesNothingAboutIt(t *testing.T) {
	admin, app, acme := acmeThings(t)
	tenant := tenancy.WithTenant(t.Context(), acme)
	for _, shape := range []struct {
		name string
		keep bool
		wait string
	}{
		{"a commit with nothing waiting", true, ""},
		{"a rollback with nothing waiting", false, ""},
		{"a commit with the work waiting for it", true, "commit"},
		{"a rollback with the work waiting for it", false, "rollback"},
	} {
		var ran int
		want := 0
		if shape.wait != "" {
			want = 1
		}
		gone := errors.New("the work was refused")
		before := committed(t, admin)
		out := droppedEffectsLog(t, func() {
			err := db.Run(tenant, app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if err := insert(tx.DB(), acme.ID, shape.name); err != nil {
					return err
				}
				switch shape.wait {
				case "commit":
					mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran++; return nil })
				case "rollback":
					mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
				}
				if shape.keep {
					return nil
				}
				return gone
			})
			if shape.keep && err != nil {
				t.Errorf("%s: Run answered %v, want nil", shape.name, err)
			}
			if !shape.keep && !errors.Is(err, gone) {
				t.Errorf("%s: Run answered %v, want the handler's own refusal", shape.name, err)
			}
		})
		if ran != want {
			t.Errorf("%s: %d effects ran, want %d", shape.name, ran, want)
		}
		if after, wantRows := committed(t, admin), boolInt(shape.keep); after-before != wantRows {
			t.Errorf("%s: the ending committed %d rows, want %d", shape.name, after-before, wantRows)
		}
		if out != "" {
			t.Errorf("%s wrote about effects it dropped, having dropped none: %s", shape.name, out)
		}
	}
}

// The COMMIT whose answer never came is the ending that drops both queues, and the line
// names every effect it dropped, not the half an outcome happened to favour. The device is
// a deferrable unique key violated twice, so the refusal can only arrive at commit.
func TestAnEndingOfUnknownOutcomeNamesEveryEffectItDropped(t *testing.T) {
	admin, app, acme := acmeThings(t)
	exec(t, t.Context(), admin, `CREATE TABLE marks (tenant_id uuid, name text, UNIQUE (tenant_id, name) DEFERRABLE INITIALLY DEFERRED)`)
	var ran int
	out := droppedEffectsLog(t, func() {
		err := db.Run(tenancy.WithTenant(t.Context(), acme), app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			for _, twice := range []string{"twice", "twice"} {
				if err := tx.DB().Exec("INSERT INTO marks VALUES (?, ?)", acme.ID, twice).Error; err != nil {
					return err
				}
			}
			mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran++; return nil })
			mustDefer(t, ctx, db.AfterCommit, func(context.Context) error { ran++; return nil })
			mustDefer(t, ctx, db.AfterRollback, func(context.Context) error { ran++; return nil })
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "SQLSTATE 23505") {
			t.Fatalf("the commit answered %v, want the deferred key's refusal", err)
		}
	})
	if ran != 0 {
		t.Errorf("%d effects ran although the COMMIT's outcome is unknown", ran)
	}
	for _, want := range []string{"effects=3", "committed=false", "rolled_back=false"} {
		if !strings.Contains(out, want) {
			t.Errorf("the line about an unanswered COMMIT does not say %s: %q", want, out)
		}
	}
}

func boolInt(kept bool) int {
	if kept {
		return 1
	}
	return 0
}
