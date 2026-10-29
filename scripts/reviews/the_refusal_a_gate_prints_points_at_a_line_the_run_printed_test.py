#!/usr/bin/env python3
"""A gate refusal that points at another line of its own run has to print that line.

`scripts/e2e.sh` refuses a machine that cannot start its probe listener with one exit status and two
possible faults — a toolchain that reported no port, and a directory with nowhere private to write the
port it was given. One number cannot tell those apart, so the gate's sentence does: it says the
`free port:` line *this run printed* names the machine that refused. That sentence is a promise about
the rest of the run's own output, and it is false whenever the run prints the sentence and no such
line — a person who follows it scans a screen for a line that is not there and finds no machine at all.

So each case runs gate 10 against one fault alone (a shadowed `node` that starts no listener; a
`TMPDIR` that cannot be written), and asks the run for both halves of the promise: the pointing
sentence and the line it points at. The pointed-at line is taken by its own prefix rather than from the
whole output, because the tool that failed prints its own diagnostic in front of it — `mktemp` names the
same directory in its own words, which would answer a weaker assertion without the script ever
naming anything. The line is then asked for the machine it is supposed to name: the directory case has
to name the directory and the setting that changes it, and the two faults must not come out in the same
words, because a pointer to an unnamed machine separates nothing.

Whichever fault it met, the run has to stop before it builds a binary, creates a database or dispatches
a browser, so every case also searches for the lines only a run that went past the probe can print.

Run: python3 -m pytest -q scripts/reviews/the_refusal_a_gate_prints_points_at_a_line_the_run_printed_test.py
"""
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
E2E = os.path.join(ROOT, "scripts", "e2e.sh")
# A Postgres that is not there on purpose: a run that got as far as the database would fail for a
# reason that has nothing to do with the listener, and the case would prove nothing.
DEAD_URL = "postgres://unused:unused@127.0.0.1:1/unused"
# What a run that got past the probe would print, and none of it may appear in a refused run.
ADVANCED = re.compile(r"a database of its own|serving on|Listing tests:|Total: .* tests")
# The line the gate's sentence points at, read by its prefix: every line `scripts/free_port.sh` says
# about the port it could not get starts this way, and no other producer in the run does.
POINTED_AT = re.compile(r"^free port: .*$", re.MULTILINE)
NO_LISTENER_NODE = "#!/bin/sh\nexit 0\n"


def gate(env):
    """Run gate 10 the way a person does, and stop it at the first question it cannot answer."""
    run = subprocess.run(["timeout", "120", "bash", E2E, "surfaces.spec.ts", "--list"],
                         capture_output=True, text=True, timeout=600, env=env, cwd=ROOT,
                         stdin=subprocess.DEVNULL)
    return {"exit": run.returncode, "said": run.stdout + run.stderr}


def env_for(tmpdir, bin_path=None, overrides=None):
    """The gate's own environment: no database behind either URL, a modelled tool first on PATH, the
    private place the run was given, and a case's own override of any of them last."""
    settings = {"TMPDIR": str(tmpdir)}
    settings.update(overrides or {})
    settings.update({"PLATFORMKIT_TEST_ADMIN_URL": DEAD_URL,
                     "PLATFORMKIT_TEST_DATABASE_URL": DEAD_URL})
    if bin_path:
        os.makedirs(bin_path, exist_ok=True)
        settings["PATH"] = "%s%s%s" % (bin_path, os.pathsep, os.environ["PATH"])
    return dict(os.environ, **settings)


def refused(gate_run, fault):
    """The run refused, refused before it touched anything, and said who it thinks is broken."""
    assert gate_run["exit"] == 1, "gate 10 answered %d where a refusal was owed; it said: %s" % (
        gate_run["exit"], gate_run["said"])
    advanced = ADVANCED.search(gate_run["said"])
    assert advanced is None, (
        "the run went past the question it refused and printed %r; a refusal has to come before the "
        "binary is built, the database created and the browser dispatched: %s" % (
            advanced.group(0), gate_run["said"]))
    assert "Nothing was built, no database was created and no browser was dispatched" in gate_run["said"], (
        "the %s refusal told nobody what the run did not go on to do: %s" % (fault, gate_run["said"]))
    return gate_run["said"]


def pointed_line(said):
    """Every `free port:` line the run printed, joined; the sentence above them promises there is one."""
    lines = POINTED_AT.findall(said)
    assert "The 'free port:' line" in said, (
        "the gate never pointed at a line of its own run; it said: " + said)
    assert lines, (
        "the gate pointed at the `free port:` line this run printed, and this run printed none, so the "
        "person following that sentence scans a screen with no machine named on it: " + said)
    return "\n".join(lines)


def test_a_toolchain_that_reports_no_port_prints_the_line_its_refusal_points_at(tmp_path):
    """A listener that never said its port: the fault line has to be there to be pointed at."""
    box = str(tmp_path / "noListener" / "bin")
    env = env_for(tmp_path / "place", box)
    with open(os.path.join(box, "node"), "w", encoding="utf-8") as handle:
        handle.write(NO_LISTENER_NODE)
    os.chmod(os.path.join(box, "node"), 0o755)
    os.makedirs(tmp_path / "place", exist_ok=True)
    said = pointed_line(refused(gate(env), "missing listener"))
    assert "no listener is running to ask about" in said, (
        "the line the gate pointed at named something other than the listener that never came up; "
        "it said: " + said)
    assert "cannot say which process is listening" not in said, (
        "the line the gate pointed at accused the socket table for a listener that never existed: " + said)


def test_a_private_place_that_cannot_be_written_prints_the_line_its_refusal_points_at(tmp_path):
    """No TMPDIR: the line has to name the directory and the setting, not just the missing listener."""
    missing = str(tmp_path / "no-such-place")
    said = pointed_line(refused(gate(env_for(tmp_path, overrides={"TMPDIR": missing})),
                                "unwritable private place"))
    assert missing in said, (
        "the line the gate pointed at did not name the directory it could not write into, so the "
        "reader has a pointer to a machine with no address: " + said)
    assert "TMPDIR" in said, (
        "the line the gate pointed at named no setting the person can change: " + said)


def test_the_two_faults_are_pointed_at_by_two_different_lines(tmp_path):
    """A pointer that says `the line names the machine` names nothing if both machines print one line."""
    box = str(tmp_path / "toolchain" / "bin")
    env_toolchain = env_for(tmp_path / "place", box)
    with open(os.path.join(box, "node"), "w", encoding="utf-8") as handle:
        handle.write(NO_LISTENER_NODE)
    os.chmod(os.path.join(box, "node"), 0o755)
    os.makedirs(tmp_path / "place", exist_ok=True)
    missing = str(tmp_path / "no-such-place")
    env_place = env_for(tmp_path, overrides={"TMPDIR": missing})
    toolchain = pointed_line(refused(gate(env_toolchain), "missing listener"))
    place = pointed_line(refused(gate(env_place), "unwritable private place"))
    assert toolchain.strip() != place.strip(), (
        "one line answered both a toolchain that started no listener and a directory that cannot be "
        "written, so the sentence pointing at it separates nothing: " + toolchain)


if __name__ == "__main__":
    raise SystemExit(subprocess.call([sys.executable, "-m", "pytest", "-q", os.path.abspath(__file__)]))
