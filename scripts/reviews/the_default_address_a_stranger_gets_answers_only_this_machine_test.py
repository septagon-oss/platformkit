#!/usr/bin/env python3
"""The address `platformkit start` answers with when nobody names one reaches no other machine.

`start` prints an administrator's password to the terminal, so the address it binds by default is a
security claim and not a preference. Three documents state it — the run's own `--addr` help, the
repository README and apps/platformkit/README.md — and a bare `:port` binds every interface the host
has, so a default that is loopback only because a resolver says so (a host *name*, or no host at all)
is one a hosts entry, a search domain or a dropped flag can move.

This asks the run itself, through `start --help`, and refuses:
  * a default whose host is not a loopback IP *literally* (a name is not an address);
  * a default on a port at or below 1024, which the README's unprivileged command could not bind;
  * a README that still promises the server "listens only on your machine" without also stating what
    an address that names no host does, since `--addr :8080` remains allowed and is how a person says
    they mean it;
  * a module README that quotes a default the command does not answer with.

Judged at this branch's own command and its own two paragraphs, so no other branch can move it.

Run: python3 -m pytest -q scripts/reviews/the_default_address_a_stranger_gets_answers_only_this_machine_test.py
"""
import ipaddress
import os
import re
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
LOOPBACK_CLAIM = "listens only on your machine"


def run_help():
    """The usage `start` prints, which carries the default it binds."""
    started = subprocess.run(
        ["go", "run", "./apps/platformkit", "start", "--help"],
        cwd=ROOT, capture_output=True, text=True, timeout=600,
    )
    usage = started.stdout + started.stderr
    assert "Usage of start:" in usage, "start answered no usage for --help:\n" + usage
    return usage


def default_addr(usage=None):
    usage = usage or run_help()
    found = re.search(r"-addr string\n\s+.*?\(default \"([^\"]+)\"\)", usage, re.S)
    assert found, "start's --addr help carries no default:\n" + usage
    return found.group(1)


def read(*parts):
    with open(os.path.join(ROOT, *parts), encoding="utf-8") as document:
        return document.read()


def test_the_default_address_is_a_loopback_address_and_not_a_name():
    """What the command binds by default is a loopback literal on an unprivileged port."""
    address = default_addr()
    host, _, port = address.rpartition(":")
    number = int(port)
    try:
        answer = ipaddress.ip_address(host)
    except ValueError:
        raise AssertionError(
            "start defaults to host %r, which is a name rather than an address: a hosts entry or a "
            "search domain can answer a name with any interface" % (host,)
        )
    assert answer.is_loopback, "start defaults to %s, and %s is not a loopback address" % (address, host)
    assert number > 1024, "start defaults to port %d, which the unprivileged command the README prints could not bind" % number


def test_the_readme_that_promises_loopback_states_what_a_bare_port_does():
    """The promise and the escape hatch are one sentence's worth of truth, not two documents' worth of hope."""
    readme = read("README.md")
    if LOOPBACK_CLAIM not in readme:
        return  # the README makes no promise about which machine answers
    assert "every interface" in readme, (
        "README.md promises the server %r while --addr still accepts an address that names no host, "
        "and the document never says what such an address binds" % LOOPBACK_CLAIM
    )


def test_the_module_readme_quotes_the_default_the_command_answers_with():
    """The two documents that state the default state the run's own answer."""
    usage = run_help()
    address = default_addr(usage)
    assert "defaults to `%s`" % address in read("apps", "platformkit", "README.md"), (
        "apps/platformkit/README.md quotes a --addr default other than the %s the command answers" % address
    )


if __name__ == "__main__":
    sys.exit(subprocess.call([sys.executable, "-m", "pytest", "-q", os.path.abspath(__file__)]))
