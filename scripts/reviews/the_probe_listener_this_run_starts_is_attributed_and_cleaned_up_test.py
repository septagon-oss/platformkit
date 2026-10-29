#!/usr/bin/env python3
"""Gate 10's listener probe is asked about its own socket, is not read from silence, and cleans up.

`scripts/free_port.sh` answers one question before `scripts/e2e.sh` builds anything: does this machine
say which process holds a listening socket? It answers by starting a listener of its own and asking the
socket table who holds it, so the answer is a property of the machine rather than of a tool's exit code.
Three things decide whether that answer can be trusted, and a machine that gets any of them wrong must be
refused rather than served:

  * an empty answer has to be distinguishable from a machine that cannot answer, so the probe asks more
    than once before it reads silence as "nobody holds this";
  * a table that prints listening lines but names no process (`ss` without `-p`, a mount of /proc that
    hides other processes) is the same silence wearing a tool's face;
  * the probe starts a listener and a file to announce its port, and a run that leaves either behind
    has started a background service it never told anyone about.

Each case asks `port_attribution_works` itself, with the machine's own socket-table tools shadowed on
PATH by models that answer in the shape named, and is judged only on what the function returns, how many
questions it asked, and what it left running. Nothing here depends on the host being able to attribute:
where the real tools cannot answer, the machine's own answer is the refusal and the cases that need an
answer compare two answers rather than insisting on one.

Run: python3 -m pytest -q scripts/reviews/the_probe_listener_this_run_starts_is_attributed_and_cleaned_up_test.py
"""
import os
import shutil
import subprocess
import sys
import tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FREE_PORT = os.path.join(ROOT, "scripts", "free_port.sh")

# The counter lives outside the directory the probe is given as TMPDIR, so what the probe leaves behind
# is the only thing in there.
COUNTER = "PK_PROBE_QUESTION_COUNT"
HARNESS = r"""
set -u
# Children are counted the same way before and after, so the shell running this harness sees itself in
# both answers and only a listener the probe failed to kill changes the number.
children() { ps -o pid= --ppid $$ 2>/dev/null | wc -l; }
before="$(children)"
# shellcheck source=/dev/null
. "$1"
port_attribution_works >/dev/null 2>&1
echo "exit=$?"
echo "asked=$(cat "${PK_COUNTER}" 2>/dev/null || echo 0)"
echo "children_before=$before"
echo "children_after=$(children)"
"""

SILENT = """#!/bin/sh
n=0; [ -f "$PK_COUNTER" ] && n=$(cat "$PK_COUNTER")
printf '%%s' "$((n + 1))" >"$PK_COUNTER"
%s
"""
# What each modelled tool answers once it has counted the question. `ss` that exits 0 printing nothing,
# `ss` that prints a listening line with no `pid=`, and `lsof` whose exit 1 is its own "no files
# selected": each is a machine that has a socket table and no answer in it.
MODELLED = {"lines": 'echo "LISTEN 0 4096 127.0.0.1:60101 0.0.0.0:*"\nexit 0',
            "silent": "exit 0",
            "silent_lsof": "exit 1"}
LATE = """#!/bin/sh
n=0; [ -f "$PK_COUNTER" ] && n=$(cat "$PK_COUNTER")
printf '%%s' "$((n + 1))" >"$PK_COUNTER"
if [ "$n" -lt "%s" ]; then exit 0; fi
exec %s "$@"
"""


def probe(mode, nth=1):
    """Run `port_attribution_works` with the machine's socket-table tools modelled, as this run's own.

    mode is "real" (nothing shadowed), "silent" (the tool answers nothing for every question), "lines"
    (listening lines with no process named) or "late" (nothing for the first `nth` questions, then the
    real tool). Returns exit status, questions asked, children of the shell afterwards, and any file the
    probe left in the TMPDIR it was given. A machine with neither ss nor lsof has nothing to shadow, and
    the run it refuses is the run the machine's own tools refuse.
    """
    tools = {t: shutil.which(t) for t in ("ss", "lsof")}
    with tempfile.TemporaryDirectory() as work:
        box, scratch = os.path.join(work, "bin"), os.path.join(work, "scratch")
        os.mkdir(box)
        os.mkdir(scratch)
        for name, found in tools.items():
            if not found:
                continue
            if mode == "late":
                body = LATE % (nth, found)
            elif mode == "lines":
                body = SILENT % MODELLED["lines"]
            elif mode == "silent":
                body = SILENT % MODELLED["silent" if name == "ss" else "silent_lsof"]
            else:
                continue
            model = os.path.join(box, name)
            with open(model, "w", encoding="utf-8") as handle:
                handle.write(body)
            os.chmod(model, 0o755)
        counter = os.path.join(work, "questions")
        env = dict(os.environ, TMPDIR=scratch, PATH="%s:%s" % (box, os.environ["PATH"]),
                   PK_COUNTER=counter, PK_NTH=str(nth))
        run = subprocess.run(["bash", "-c", HARNESS, "-", FREE_PORT], capture_output=True,
                             text=True, timeout=600, env=env)
        said = dict(line.split("=", 1) for line in run.stdout.split("\n") if "=" in line)
        assert said.get("exit"), "the probe answered nothing at all: " + run.stdout + run.stderr
        left = sorted(entry for entry in os.listdir(scratch) if entry != "bin")
        return {"exit": int(said["exit"]), "asked": int(said["asked"]),
                "before": int(said["children_before"]), "after": int(said["children_after"]),
                "left": left}


def test_a_socket_table_that_never_names_the_probe_listener_is_refused_after_more_than_one_question():
    """Silence about a listener this run started is refused, and refused only after asking again."""
    answer = probe("silent")
    assert answer["exit"] == 1, "gate 10 would serve a browser through a listener it cannot name"
    assert answer["asked"] > 1, (
        "the probe accepted the first empty answer: %d question(s) about a listener this run started "
        "is not enough to tell a machine that has no listener from one that cannot answer" % answer["asked"]
    )


def test_a_socket_table_that_prints_listening_lines_but_names_no_process_is_refused():
    """`ss` that lists the socket and hides the process is the same silence wearing a tool's face."""
    answer = probe("lines")
    assert answer["exit"] == 1, (
        "the probe read a listening line as an answer and asked nothing of the process behind it"
    )


def test_an_owner_named_only_after_the_first_question_is_believed_the_way_an_early_one_is():
    """A machine that attributes answers alike whether or not the table was slow the first time."""
    own = probe("real")
    late = probe("late", nth=2)
    assert late["exit"] == own["exit"], (
        "this machine attributes %s when the socket table names the owner on the second question and "
        "%s when it names it on the first: a run that moved its own port would be refused for the "
        "slowness of the table rather than for anything about the port"
        % ("can" if late["exit"] == 0 else "cannot", "can" if own["exit"] == 0 else "cannot")
    )


def test_the_probe_leaves_no_listener_running_and_nothing_written():
    """The listener and the file that carried its port belong to the probe and die with it."""
    for mode in ("real", "silent"):
        answer = probe(mode)
        assert answer["after"] <= answer["before"], (
            "the probe left a child of the shell that ran it (%d before, %d after): a run that cannot "
            "say whose listener it is has no business leaving one running" % (answer["before"], answer["after"])
        )
        assert answer["left"] == [], "the probe left %s behind in the directory it was given" % answer["left"]


if __name__ == "__main__":
    raise SystemExit(subprocess.call([sys.executable, "-m", "pytest", "-q", os.path.abspath(__file__)]))
