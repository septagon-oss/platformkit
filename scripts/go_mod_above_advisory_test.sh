#!/usr/bin/env bash
# The two versions go.mod fixes sit outside the ranges the Go advisories name.
#
# Why this file exists. `.gitea/workflows/ci.yml`'s `govulncheck` step is the only Go security gate in
# this repository and it needs the network, so `make check` does not run it — a laptop without one can
# still gate a pull request. That is why forge run 55758 (head 249a3a07, 2026-10-09T04:22:50Z) could
# come back `failure` on `check / govulncheck` over a tree whose `make check` had passed minutes
# earlier, and why the loop read that as the task's own defect. What the step refused:
#
#   golang.org/x/net@v0.59.0   GO-2026-6617, -6612, -6611, -6610, -6603   fixed in v0.60.0
#   net/http@go1.27.1          GO-2026-6613, -6609, -6605                 fixed in go1.27.2
#   net/http/internal/http2    GO-2026-6617, -6612, -6611, -6610, -6603   fixed in go1.27.2
#   net/textproto@go1.27.1     GO-2026-6608                               fixed in go1.27.2
#   crypto/tls@go1.27.1        GO-2026-6607                               fixed in go1.27.2
#   html/template@go1.27.1     GO-2026-6600, -6599                        fixed in go1.27.2
#
# and nothing in the repository had moved: forge run 54902 on the merge base passes the same step on
# the same two versions (`git diff 59d7f4d 249a3a07 -- go.mod go.sum` answers only this branch's own
# lines). The advisory database moved under the runner — the same event
# `scripts/openpencil_lock_above_advisory_test.sh` records for npm's feed over a byte-identical lock,
# and this file is the Go half of that record.
#
# What a tree can answer about a feed it does not hold is which side of a published range its own
# pinned versions land on, which is all of it asked here and none of it asked of the network. The list
# below is transcribed from the run's own log, with the identifier and the fixed release the tool
# printed for each; it is a floor, so a release that supersedes the fix passes without any edit here,
# and a downgrade to a version inside a range is refused. When a new advisory lands, whoever acts on
# the CI step adds its entry here as they raise the pin — the two lines move together or the case is
# red, which is the point.
#
# The toolchain is a version this file can compare because `.gitea/workflows/ci.yml` installs Go with
# `go-version-file: go.mod`, which reads go.mod's `toolchain` line, so that line names the standard
# library the scan runs against. It is not the `go` line: that one is the floor a consumer of this
# module clears (`Makefile`'s own comment says as much), and a check of it here would refuse a
# perfectly safe consumer for a version nobody in this repository compiles with.
#
# Both verdicts go through one function each and the mutations at the end feed those same functions, so
# a version printed as "sits above" without the comparison behind it cannot print `ok` about a go.mod
# nobody compared — the failure mode `scripts/openpencil_lock_above_advisory_test.sh` documents and
# pins against.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
if [ -n "${1:-}" ]; then
	root="$(cd "$1" && pwd)"
fi

python3 - "$root" <<'PY'
import os
import re
import shutil
import sys
import tempfile
from pathlib import Path

root = sys.argv[1]

# `fixed` is the release the scan prints as `Fixed in:`; every id sharing it is closed by that release.
ADVISORIES = (
    {
        "package": "golang.org/x/net",
        "source": "module",
        "fixed": (0, 60, 0),
        "ids": ("GO-2026-6603", "GO-2026-6610", "GO-2026-6611", "GO-2026-6612", "GO-2026-6617"),
    },
    {
        "package": "toolchain",
        "source": "toolchain",
        "fixed": (1, 27, 2),
        "ids": ("GO-2026-6599", "GO-2026-6600", "GO-2026-6605", "GO-2026-6607", "GO-2026-6608",
                "GO-2026-6609", "GO-2026-6613"),
    },
)


def parse_release(text):
    """The three numbers of `v0.60.0` or `go1.27.2`, or None for anything this comparison cannot place.

    A pre-release or architecture suffix (`go1.27.2X:nodwarf5`, which is what a patched local toolchain
    answers) keeps its leading numbers and drops the suffix: the advisory ranges are stated in plain
    releases, and a release is what both sides of this comparison are made of.
    """
    match = re.search(r"(?:go|v)(\d+)\.(\d+)\.(\d+)", text or "")
    return tuple(int(x) for x in match.groups()) if match else None


def go_mod_lines(root):
    """go.mod without comment blocks: a commented-out dependency is not a pin.

    Only the shape this repository uses is read — one dependency per line inside a `require (` block —
    because that is what `go mod tidy` writes and `make check`'s `go mod tidy -diff` step keeps it as.
    """
    return [line.split("//")[0].rstrip() for line in (Path(root) / "go.mod").read_text().splitlines()]


def pinned(root, package):
    for line in go_mod_lines(root):
        fields = line.split()
        if len(fields) == 2 and fields[0] == package:
            return fields[1]
    return None


def toolchain(root):
    for line in go_mod_lines(root):
        fields = line.split()
        if len(fields) == 2 and fields[0] == "toolchain":
            return fields[1]
    return None


def release(fixed, source):
    """The fix as the scan prints it: `go1.27.2` for a standard library, `v0.60.0` for a module."""
    return ("go" if source == "toolchain" else "v") + ".".join(str(x) for x in fixed)


def verdict(root, advisory):
    """None when the pin sits at or above the fix, the sentence a person reads when it does not."""
    pinned_version = toolchain(root) if advisory["source"] == "toolchain" else pinned(root, advisory["package"])
    where = "go.mod's toolchain line" if advisory["source"] == "toolchain" else f"go.mod's {advisory['package']}"
    fix = release(advisory["fixed"], advisory["source"])
    found = parse_release(pinned_version)
    if found is None:
        missing = ("names no toolchain, so the forge's setup-go installs the `go` floor and this file "
                   "cannot say which standard library the scan ran against"
                   if advisory["source"] == "toolchain" else "is absent, so nothing here resolves above the range")
        return f"FAIL: {where} {missing}; {' '.join(advisory['ids'])} want {fix} or newer"
    if found < advisory["fixed"]:
        return (f"FAIL: {where} is pinned {pinned_version}, which "
                f"{', '.join(advisory['ids'])} report as vulnerable; "
                f"{fix} is the release that closes them")
    return None


failures = []
for advisory in ADVISORIES:
    refusal = verdict(root, advisory)
    pinned_version = toolchain(root) if advisory["source"] == "toolchain" else pinned(root, advisory["package"])
    if refusal:
        print(refusal)
        failures.append(refusal)
    else:
        print(f"ok   {advisory['package']} is pinned {pinned_version}, outside the ranges "
              f"{', '.join(advisory['ids'])} name")

# The mutations: go.mod copies with one pin moved back inside a range, fed through the same `verdict`.
# Without these, a comparison that always answers None — an inverted `<`, a missing entry — prints the
# same `ok` lines this run just printed about a tree that CI would refuse.


def mutated(edit):
    """Path to a throwaway repository holding go.mod with `edit` applied to a copy of the committed one."""
    directory = tempfile.mkdtemp(prefix="pkit-go-mod-advisory-")
    shutil.copy(os.path.join(root, "go.mod"), os.path.join(directory, "go.mod"))
    path = os.path.join(directory, "go.mod")
    with open(path) as handle:
        text = handle.read()
    with open(path, "w") as handle:
        handle.write(edit(text))
    return directory


MUTATIONS = (
    ("x/net back inside the range",
     lambda t: re.sub(r"golang\.org/x/net v[\d.]+", "golang.org/x/net v0.59.0", t)),
    ("the toolchain back inside the range",
     lambda t: re.sub(r"(?m)^toolchain go[\d.]+$", "toolchain go1.27.1", t)),
    ("no toolchain line at all",
     lambda t: re.sub(r"(?m)^toolchain go[\d.]+$", "", t)),
    ("x/net not pinned anywhere",
     lambda t: re.sub(r"(?m)^\tgolang\.org/x/net v[\d.]+$", "", t)),
    ("the pins intact",
     lambda t: t),
)

accepted = 0
for label, edit in MUTATIONS:
    directory = mutated(edit)
    try:
        refused = [r for r in (verdict(directory, a) for a in ADVISORIES) if r]
    finally:
        shutil.rmtree(directory, ignore_errors=True)
    should_refuse = not label.endswith("intact")
    if should_refuse and not refused:
        print(f"FAIL: the guard accepted {label}:")
        failures.append(label)
    elif should_refuse:
        print(f"ok   {label}: refused — {refused[0]}")
    elif refused:
        print(f"FAIL: the guard refused the unmutated copy: {refused[0]}")
        failures.append(label)
    else:
        accepted += 1
        print(f"ok   {label}: accepted")

if accepted != 1:
    print("FAIL: the accepted shape never reached the guard")
    failures.append("accepted shape")

if failures:
    sys.exit(1)
PY

echo "go.mod above advisory: the toolchain and golang.org/x/net this repository fixes sit outside every range the run names, and a pin back inside one is refused"
