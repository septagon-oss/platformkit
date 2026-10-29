#!/usr/bin/env python3
"""An expanded plan that is written through leaves the manifest it was built from alone.

`module.Expand` copies the manifest list and completes the two facts the kernel can
answer for it — the module a subscription belongs to, and the module part of a nav
screen named by its entity alone. Its stated promise is that input slices are not
mutated, and two consumers read the completed lists long before any router exists:
pkit's plan and the durable consumer name of every subscription. So a plan built
from a manifest has to be the plan's own, and a later write to it has to stay in it.

The two fill helpers used to decide whether to copy by looking for an entry that
needed filling, and returned the list they were given when none did — which is the
shape every production manifest that names its own module on every subscription
arrives in. That window is what this refuses: it builds a manifest whose entries are
all already named, expands it, writes through the expanded subscriptions and nav, and
asks the manifest whether anything arrived.

The Go case is compiled into `kit/module` through `go test -overlay`, so the case runs
inside the package it checks — it needs the package's own `handler` fixture and the
unexported fill helpers — without a file added to the repository. The overlay maps a
package path that does not exist on disk to this file's own temporary directory.

Run: python3 -m pytest -q scripts/reviews/the_expanded_plan_writes_nothing_through_the_manifest_it_was_given_test.py
"""
import json
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PACKAGE = os.path.join("kit", "module")
CASE = "TestTheExpandedPlanWritesNothingThroughTheManifestItWasGiven"

# Written as the package's own test file so it compiles beside the fill helpers it
# checks. Two manifests, both of them whole: every subscription already names a
# module (its own in one, another's in the other) and every nav screen already names
# its module, so neither list has an entry that needs filling. That is precisely the
# case a copy decided by "does anything need changing" gets wrong.
GO_CASE = '''package module

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
)

func ''' + CASE + '''(t *testing.T) {
	manifests := []struct {
		name string
		mod  Module
	}{
		{"every subscription names this module", Module{
			Name:   "task",
			Events: []string{"task.task.created"},
			Subscriptions: []events.Subscription{
				{Module: "task", Name: "task.task.created", Handler: handler},
			},
			Nav: []NavEntry{{Label: "Tasks", Screen: "task/tasks"}},
		}},
		{"every subscription names another module", Module{
			Name:   "task",
			Events: []string{"task.task.created"},
			Subscriptions: []events.Subscription{
				{Module: "billing", Name: "task.task.created", Handler: handler},
			},
			Nav: []NavEntry{{Label: "Plans", Screen: "billing/plans"}},
		}},
	}
	for _, tc := range manifests {
		t.Run(tc.name, func(t *testing.T) {
			wantSub := tc.mod.Subscriptions[0].Name
			wantLabel := tc.mod.Nav[0].Label
			manifest := tc.mod
			plan := Expand([]Module{manifest})
			if len(plan) != 1 || len(plan[0].Subscriptions) == 0 || len(plan[0].Nav) == 0 {
				t.Fatalf("the expansion answered %d modules, want one that carries both lists", len(plan))
			}
			plan[0].Subscriptions[0].Name = "renamed through the plan"
			plan[0].Nav[0].Label = "relabelled through the plan"
			if got := manifest.Subscriptions[0].Name; got != wantSub {
				t.Errorf("writing the expanded plan reached the manifest it was built from: its subscription name went from %q to %q", wantSub, got)
			}
			if got := manifest.Nav[0].Label; got != wantLabel {
				t.Errorf("writing the expanded nav reached the manifest it was built from: its entry label went from %q to %q", wantLabel, got)
			}
		})
	}
}
'''


def run_case(tmp_path):
    """Compile the Go case into kit/module and run it, returning the tool's answer.

    The case arrives through an overlay so nothing is added to the repository: the
    package path the overlay names exists only for that build.
    """
    source = tmp_path / "expanded_plan_writes_nothing_test.go"
    source.write_text(GO_CASE, encoding="utf-8")
    overlay = tmp_path / "overlay.json"
    overlay.write_text(
        json.dumps({"Replace": {os.path.join(ROOT, PACKAGE, source.name): str(source)}}),
        encoding="utf-8",
    )
    proc = subprocess.run(
        ["go", "test", "./" + PACKAGE + "/", "-count=1", "-overlay=" + str(overlay),
         "-run", CASE, "-v"],
        cwd=ROOT, capture_output=True, text=True, timeout=600, check=False,
    )
    return proc


def test_the_expanded_plan_writes_nothing_through_the_manifest_it_was_given(tmp_path):
    proc = run_case(tmp_path)
    # A filter that matched nothing exits 0 with a warning, which would read as a
    # pass: the case has to be seen running by name before its verdict is believed.
    assert "RUN   " + CASE in proc.stdout, "the case never ran:\n" + proc.stdout + proc.stderr
    assert "no tests to run" not in proc.stdout, proc.stdout
    assert proc.returncode == 0, "the expanded plan wrote through its manifest:\n" + proc.stdout + proc.stderr


if __name__ == "__main__":
    import tempfile
    from pathlib import Path

    with tempfile.TemporaryDirectory() as d:
        completed = run_case(Path(d))
        sys.stdout.write(completed.stdout + completed.stderr)
        sys.exit(completed.returncode)
