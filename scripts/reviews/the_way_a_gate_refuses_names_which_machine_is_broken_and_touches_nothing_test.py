#!/usr/bin/env python3
"""Gate 10 refuses for the machine that is really broken, and each refusal reaches a different fault.

`scripts/free_port.sh` answers one question before `scripts/e2e.sh` builds anything — can this machine
say which process holds a listening socket? — and it answers with two refusals, because two machines
fail that question in two ways:

  * the socket table is mute: a listener this run started itself is up and nothing will name the
    process behind it, which is the machine gate 10 exists to refuse;
  * the toolchain starts no listener: nothing ever came into existence to be asked about, so the
    socket table was never consulted and has not been proved guilty of anything.

A run that reads the second fault as the first sends the person to iproute2 and /proc, and a run that
reads the first as the second hands a browser an address it cannot prove it serves. So each case models
one fault alone — the socket table by shadowing `ss` and `lsof` with silent executables, the toolchain
by shadowing `node` with one that starts no listener, the private place by naming a `TMPDIR` that does
not exist — and asks both `port_attribution_works` and gate 10 itself. Each is judged on the exit status,
on whether node was started at all, and on the words the refusal used; nothing is judged on a message
this repository does not print.

Whichever fault it met, gate 10 refuses before it builds a binary, creates a database or dispatches a
browser, and that is the second half of every case here: the run's own output is searched for the lines
only a run that went past the probe can print.

Run: python3 -m pytest -q scripts/reviews/the_way_a_gate_refuses_names_which_machine_is_broken_and_touches_nothing_test.py
"""
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FREE_PORT = os.path.join(ROOT, "scripts", "free_port.sh")
E2E = os.path.join(ROOT, "scripts", "e2e.sh")
# A Postgres that is not there on purpose: a run that got as far as the database would fail for a reason
# that has nothing to do with the socket table, and the case would prove nothing.
DEAD_URL = "postgres://unused:unused@127.0.0.1:1/unused"

# What went past the probe would print, and none of it may appear in a refused run's output.
ADVANCED = re.compile(r"a database of its own|serving on|Listing tests:|Total: .* tests")

SILENT_SS = "#!/bin/sh\nexit 0\n"          # a socket table with no ownership information in it
SILENT_LSOF = "#!/bin/sh\nexit 1\n"        # lsof's own "no files selected": the same silence
NO_LISTENER_NODE = "#!/bin/sh\nexit 0\n"   # a toolchain that starts no listener at all
COUNTING_NODE = ('#!/bin/sh\n'
                 'printf 1 >>"$PK_NODE_STARTS"\n'
                 'exit 0\n')


def shadow(box, **tools):
    """Put an executable per named tool in `box`, ahead of the real PATH, and return that directory."""
    os.makedirs(box, exist_ok=True)
    for name, body in tools.items():
        path = os.path.join(box, name)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(body)
        os.chmod(path, 0o755)
    return box


def with_path(box, extra=None):
    """This run's environment with `box` first on PATH, plus anything else a case needs."""
    env = dict(os.environ, PATH="%s%s%s" % (box, os.pathsep, os.environ["PATH"]))
    env.update(extra or {})
    return env


def ask_probe(env):
    """Run `port_attribution_works` alone, as the sourced file the gate uses it."""
    run = subprocess.run(["bash", "-c", '. "$1"; port_attribution_works', "-", FREE_PORT],
                         capture_output=True, text=True, timeout=600, env=env, cwd=ROOT)
    return {"exit": run.returncode, "said": run.stdout + run.stderr}


def ask_gate(env):
    """Run gate 10 the way a person does, and stop it at the first question it cannot answer."""
    run = subprocess.run(["timeout", "120", "bash", E2E, "surfaces.spec.ts", "--list"],
                         capture_output=True, text=True, timeout=600, env=env, cwd=ROOT,
                         stdin=subprocess.DEVNULL)
    return {"exit": run.returncode, "said": run.stdout + run.stderr}


def refuse_env(tmpdir, box, extra=None):
    """The gate's own environment: no database behind either URL, the models first on PATH, the private
    place the run was given, and a case's own override of any of them last."""
    settings = {"TMPDIR": str(tmpdir)}
    settings.update(extra or {})
    settings.update({"PLATFORMKIT_TEST_ADMIN_URL": DEAD_URL,
                     "PLATFORMKIT_TEST_DATABASE_URL": DEAD_URL})
    return with_path(box, settings)


def assert_touched_nothing(gate):
    """A refusal is a refusal only if the run never went past the question it could not answer."""
    assert gate["exit"] == 1, "gate 10 answered %d where a refusal was owed; it said: %s" % (
        gate["exit"], gate["said"])
    advanced = ADVANCED.search(gate["said"])
    assert advanced is None, (
        "the run went past the question it refused and printed %r; a refusal has to come before the "
        "binary is built, the database created and the browser dispatched: %s" % (
            advanced.group(0), gate["said"]))


def test_a_socket_table_that_names_no_process_is_refused_as_the_socket_table_s_fault(tmp_path):
    """A listener is up and nothing says who holds it: the fault is the socket table's, exit 1."""
    box = shadow(str(tmp_path / "mute"), ss=SILENT_SS, lsof=SILENT_LSOF)
    probe = ask_probe(with_path(box))
    assert probe["exit"] == 1, (
        "a socket table that named no holder for a listener this run started answered %d; only exit 1 "
        "tells the person to look at ss and /proc, and only exit 1 leaves the toolchain out of it: %s"
        % (probe["exit"], probe["said"]))
    gate = ask_gate(refuse_env(tmp_path, box))
    assert_touched_nothing(gate)
    assert "this machine cannot say which process is listening" in gate["said"], (
        "that refusal did not name the socket table; it said: " + gate["said"])
    assert "no listener could be started" not in gate["said"], (
        "a socket table that was never able to answer was reported as a toolchain that started no "
        "listener, and the person reading it goes to node: " + gate["said"])


def test_a_toolchain_that_starts_no_listener_is_refused_as_the_toolchain_s_fault(tmp_path):
    """Nothing came up to be asked about: the fault is the toolchain's, exit 2, and the table is clear."""
    box = shadow(str(tmp_path / "noListener"), node=NO_LISTENER_NODE)
    probe = ask_probe(with_path(box))
    assert probe["exit"] == 2, (
        "a toolchain that started no listener answered %d; exit 2 is the only answer that keeps the "
        "socket table out of the accusation: %s" % (probe["exit"], probe["said"]))
    gate = ask_gate(refuse_env(tmp_path, box))
    assert_touched_nothing(gate)
    assert "no listener could be started" in gate["said"], (
        "that refusal did not say the listener was missing; it said: " + gate["said"])
    assert "cannot say which process is listening" not in gate["said"], (
        "a toolchain that never started a listener was refused as a mute socket table: " + gate["said"])


def test_a_private_place_that_does_not_exist_refuses_before_node_is_asked_anything(tmp_path):
    """No TMPDIR is neither a mute socket table nor a broken toolchain, and still stops the run.

    The probe writes the port it was given somewhere private, so a `TMPDIR` that cannot be written is
    the third machine that cannot answer. It has to refuse — an announcement no one can read would leave
    the ownership question unasked — and it has to refuse without starting node, so the answer the
    person gets is about the directory rather than about the toolchain.
    """
    missing = str(tmp_path / "no-such-place")
    starts = str(tmp_path / "node-starts")
    box = shadow(str(tmp_path / "countingNode"), node=COUNTING_NODE)
    env = refuse_env(tmp_path, box, {"TMPDIR": missing, "PK_NODE_STARTS": starts})
    probe = ask_probe(env)
    assert probe["exit"] == 2, (
        "with nowhere private to write its port the probe answered %d; it cannot ask who holds a "
        "listener it cannot announce, so a run it serves would be a run through an unnamed socket: %s"
        % (probe["exit"], probe["said"]))
    assert not os.path.exists(starts), (
        "the probe started node before it knew whether it had anywhere to write, so the refusal a "
        "person reads about their TMPDIR is followed by a toolchain they did not ask for")
    assert "no private place" in probe["said"], (
        "the refusal did not name the place it could not write; it said: " + probe["said"])
    gate = ask_gate(env)
    assert_touched_nothing(gate)
    assert not os.path.exists(starts), (
        "gate 10 started node with no private place to write to: the fault is the directory, and the "
        "run that says otherwise sends the person to the toolchain")


def test_the_two_faults_do_not_come_out_of_the_gate_in_the_same_words(tmp_path):
    """Two machines are broken here; a person who reads one sentence cannot tell which one they have."""
    mute_box = shadow(str(tmp_path / "mute" / "bin"), ss=SILENT_SS, lsof=SILENT_LSOF)
    toolchain_box = shadow(str(tmp_path / "toolchain" / "bin"), node=NO_LISTENER_NODE)
    mute_lines = ask_gate(refuse_env(str(tmp_path / "mute"), mute_box))["said"]
    toolchain_lines = ask_gate(refuse_env(str(tmp_path / "toolchain"), toolchain_box))["said"]
    assert mute_lines.strip() != toolchain_lines.strip(), (
        "gate 10 answered the same words for a mute socket table and for a toolchain that started no "
        "listener: " + mute_lines)
    for name, said in (("socket table", mute_lines), ("toolchain", toolchain_lines)):
        assert said.strip(), "gate 10 refused for the %s without saying anything: %s" % (name, said)


if __name__ == "__main__":
    raise SystemExit(subprocess.call([sys.executable, "-m", "pytest", "-q", os.path.abspath(__file__)]))
