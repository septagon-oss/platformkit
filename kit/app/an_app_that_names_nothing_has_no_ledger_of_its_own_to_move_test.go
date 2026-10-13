package app

// The move, its record and its retry all belong to the deployment that names its
// app, and these three cases say so of the composition rather than of the step.
//
// kit/events/MoveLedger answers with the zero report when no app is named:
// appname.DurablePrefix is the empty string, so there is no scoped name to rename a
// handled row onto, and an unscoped row "moved" under the empty prefix would be the
// same row copied over itself — and then deleted, which is the write that takes the
// last claim away from the app that does hold it. A composition that cannot emit an
// event does not declare it (a channel nobody publishes is a channel an integrator
// waits on), and a step that can never act on any input the process can hand it is
// not scheduled work. TestTheBootLineNamesTheEventSchemaCoverage pins the app-less
// figure at 3/4; the third case below pins the named one, so the first cannot be
// satisfied by a line that stopped counting the fourth declaration at all.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
)

// TestTheKernelDeclaresTheMoveOnlyForAnAppThatNamesItself: the manifest, and the
// document rendered from a hand-built list, both key the fourth kernel event off
// the app the composition named.
func TestTheKernelDeclaresTheMoveOnlyForAnAppThatNamesItself(t *testing.T) {
	for _, tc := range []struct {
		app  appname.Name
		want bool
	}{
		{appname.Name(""), false},
		{appname.Name("collect"), true},
	} {
		found := func(list []events.Declared) bool {
			for _, d := range list {
				if d.Name == events.EventLedgerMoved {
					return d.Schema() != nil
				}
			}
			return false
		}
		if got := found(kernelModule(tc.app).Declared); got != tc.want {
			t.Errorf("kernelModule(%q) declares %s with a payload type = %v, want %v", tc.app, events.EventLedgerMoved, got, tc.want)
		}
		if got := found(declaredEvents(withKernelFor(tc.app, nil))); got != tc.want {
			t.Errorf("the generator's list for app %q carries %s = %v, want %v", tc.app, events.EventLedgerMoved, got, tc.want)
		}
	}
}

// TestTheMoveIsScheduledOnlyByAnAppThatNamesItself: the retry job arrives for the
// deployment that has a ledger to move and for no other, and when it arrives it is
// the retry the comment above kernelJobs describes.
func TestTheMoveIsScheduledOnlyByAnAppThatNamesItself(t *testing.T) {
	for _, tc := range []struct {
		app  appname.Name
		want int
	}{
		{appname.Name(""), 0},
		{appname.Name("collect"), 1},
	} {
		n := 0
		for _, j := range kernelJobs(memory.New(), tc.app) {
			if j.Name == "ledger-move" {
				n++
				if j.Every != ledgerMoveEvery {
					t.Errorf("app %q: the move runs every %s, want the retry's %s", tc.app, j.Every, ledgerMoveEvery)
				}
			}
		}
		if n != tc.want {
			t.Errorf("app %q schedules %d ledger-move jobs, want %d", tc.app, n, tc.want)
		}
	}
}

// TestTheBootLineCountsTheMoveForAnAppThatNamesItself boots the same composition
// the app-less figure is read from, with nats.app's slug set, and reads 4/5: the
// kernel's four covered declarations and hello's one uncovered declaration. A
// composition that names itself gains the move's declaration, and the number the
// pillar is measured by moves with it — which is what says the app-less 3/4 is a
// count of that composition and not a constant.
func TestTheBootLineCountsTheMoveForAnAppThatNamesItself(t *testing.T) {
	cfg, opts := compose(t)
	opts.App = appname.Name("collect")
	var logs strings.Builder
	opts.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	a, err := New(t.Context(), cfg, []module.Module{hello()}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	waitFor(t, cfg.Server.Addr)
	cancel()
	if err := <-stopped; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if line := coverageLine(logs.String()); line == "" {
		t.Fatalf("the boot log named no event_schema_coverage:\n%s", logs.String())
	} else if !strings.Contains(line, "event_schema_coverage=4/5") {
		t.Errorf("the boot line of an app that names itself reads %q, want event_schema_coverage=4/5 "+
			"for the kernel's four covered declarations (the move's among them) and hello's one uncovered one", line)
	}
}

// coverageLine is the one boot line that carries the figure, last written first.
func coverageLine(logs string) string {
	var line string
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, "event_schema_coverage") {
			line = l
		}
	}
	return line
}
