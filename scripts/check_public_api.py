#!/usr/bin/env python3
"""Report exported API incompatibilities between two committed source revisions."""
import argparse
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parent.parent
TOOL = "golang.org/x/exp/cmd/apidiff@v0.0.0-20260908205506-85c1c2202aba"

# Asking the module proxy for the pinned tool is the one step in this gate that needs a
# network: `go run <module>@<version>` fetches the module's retirement notice before it
# builds anything, and on a host whose resolver has stopped answering for a few seconds
# that single lookup ends the whole check — `loading deprecation for golang.org/x/exp:
# … dial tcp: lookup proxy.golang.org: i/o timeout`, which is a sentence about the host
# and not about anybody's exported API. So a failure to reach the proxy is tried again
# after a wait instead of ending the run. This cannot swallow a finding: with
# `-m -incompatible` apidiff exits 0 whatever it finds (cmd/apidiff/main.go exits 1 only
# when it cannot write its report), and this gate's verdict is the baseline diff below,
# which no retry touches. Any other non-zero exit is reported on the first try.
FETCH_MARKERS = ("loading deprecation", "dial tcp", "no such host", "i/o timeout",
                 "connection refused", "server misbehaving", "bad gateway",
                 "service unavailable", "gateway time-out", "unexpected eof",
                 "eof while", "tls:", "context deadline exceeded")
# One wait per extra try: two seconds of retrying an eight-second lookup buys about
# nothing, so the second wait is long enough to sit out a resolver blip of the size the
# loop's own logs show, and the whole retry is still well inside the minute this check
# costs when the network is answering.
FETCH_WAITS = (5, 20)


def fetch_wait(after, output):
    """Seconds to wait before try `after + 2`, or None when this failure is final.

    The markers are the ways `go run <module>@<version>` says it could not reach a
    module proxy or the sumdb: a refused or timed-out dial, a name that did not resolve,
    a proxy that answered 5xx or broke the connection. Anything else that ends one of
    these commands non-zero — a package that will not compile, an export file that will
    not parse — is a fact about the tree and is reported on the first try, with the
    command's own output. The bound is here rather than in the caller's loop so that
    scripts/check_public_api_fetch_test.sh can pin it: a gate that retried without ever
    giving up would be a gate that hangs.
    """
    if after >= len(FETCH_WAITS) or not any(marker in output.lower() for marker in FETCH_MARKERS):
        return None
    return FETCH_WAITS[after]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base", help="supported stable tag or reviewed commit")
    parser.add_argument("candidate", nargs="?", default="HEAD", help="committed candidate; excludes working-tree edits")
    parser.add_argument("--baseline", type=Path, help="reviewed JSON list of known incompatibilities; exit on new or vanished entries rather than on the whole report, so a documented breaking line does not hide an accidental one")
    parser.add_argument("--write-baseline", action="store_true", help="write the measured report to --baseline instead of comparing; the resulting diff is the review, so run it deliberately and commit it alone")
    args = parser.parse_args()
    directive = re.search(r"(?m)^go (\S+)$", (ROOT / "go.mod").read_text())
    if directive is None:
        raise RuntimeError("source go.mod has no Go version directive")
    go_version = directive.group(1)
    env = dict(os.environ, GOENV="off", GOWORK="off", GOTOOLCHAIN="go" + go_version)
    selected = subprocess.check_output(["go", "env", "GOROOT"], env=env, text=True).strip()
    go = str(Path(selected) / "bin/go")
    env.update(
        PATH=str(Path(selected) / "bin") + os.pathsep + env.get("PATH", ""),
        GOTOOLCHAIN="local", GOFLAGS="-p=4 -buildvcs=false",
    )
    evidence = {"tool": TOOL, "go_version": go_version, "commits": {}, "commands": []}

    def run(command, cwd):
        # Every try is the same command in the same directory against the same pinned
        # tool; only whether the proxy answered differs. `tries` in the evidence is the
        # difference between a quiet run and one that got there on the third attempt,
        # which is what a reader needs to weigh the result later.
        waits = 0
        while True:
            result = subprocess.run(command, cwd=cwd, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
            if result.returncode == 0:
                evidence["commands"].append({"arguments": command, "exit_code": 0,
                                             "fetch_retries": waits})
                return result.stdout
            refused = (result.stdout + result.stderr).strip()
            wait = fetch_wait(waits, refused)
            evidence["commands"].append({"arguments": command, "exit_code": result.returncode,
                                         "fetch_retries": waits})
            if wait is None:
                raise RuntimeError(refused)
            waits += 1
            print(f"+ retry {waits} after a module fetch that did not answer, waiting {wait}s: "
                  f"{refused.splitlines()[-1] if refused else 'no output'}",
                  file=sys.stderr, flush=True)
            time.sleep(wait)

    with tempfile.TemporaryDirectory(prefix="platformkit-api-check-") as directory:
        temporary = Path(directory)
        for label, revision in (("base", args.base), ("candidate", args.candidate)):
            commit = run(["git", "rev-parse", "--verify", "--end-of-options", revision + "^{commit}"], ROOT).strip()
            evidence["commits"][label] = commit
            source = temporary / label
            source.mkdir()
            data = subprocess.check_output(["git", "archive", commit], cwd=ROOT)
            with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
                archive.extractall(source, filter="data")
            declaration = json.loads(run([go, "mod", "edit", "-json"], source))
            if declaration.get("Replace"):
                raise RuntimeError("release API snapshots must not use local or module replacements")
            print("+ export " + label + " " + commit, file=sys.stderr, flush=True)
            run([go, "run", TOOL, "-m", "-w", str(temporary / (label + ".export")), declaration["Module"]["Path"]], source)
        output = run([go, "run", TOOL, "-m", "-incompatible", str(temporary / "base.export"), str(temporary / "candidate.export")], temporary)
        report = "\n".join(line for line in output.splitlines() if not line.startswith("Ignoring internal package ")).strip()
        evidence["incompatibilities"] = report
    evidence["temporary_fixture_removed"] = True
    reported = [line[2:] if line.startswith("- ") else line for line in report.splitlines() if line]
    if args.write_baseline:
        if not args.baseline:
            raise RuntimeError("--write-baseline needs the --baseline path it is writing")
        evidence["result"] = f"baseline written with {len(reported)} incompatibilities"
        evidence["incompatibilities"] = "\n".join("- " + line for line in reported)
        print(json.dumps(evidence, indent=2))
        args.baseline.write_text(json.dumps({
            "note": "Exported API changes already accepted on this release line, one per line, in the form "
                    "apidiff reports them. This is not a licence: an entry is a change somebody reviewed and "
                    "recorded, the gate fails on any line not listed here and on any listed line that stops "
                    "being reported, and the whole measured report stays in the evidence JSON. Delete this file "
                    "when the next stable line is published, and re-measure against that tag instead.",
            "commits": evidence["commits"],
            "incompatibilities": sorted(reported),
        }, indent=2) + "\n")
        return 0
    if args.baseline:
        reviewed = json.loads(args.baseline.read_text())
        known = [line[2:] if line.startswith("- ") else line for line in reviewed["incompatibilities"] if line]
        # Two directions, both of them a review request. A line the baseline does not
        # name is an incompatibility nobody agreed to. A line the baseline names and
        # the report no longer produces means the baseline is measuring something that
        # changed underneath it — the same rule check_budget_ratchet.sh applies to a
        # removed bucket, because a baseline that cannot go stale is a list of excuses.
        new = sorted(set(reported) - set(known))
        stale = sorted(set(known) - set(reported))
        shown = args.baseline.relative_to(ROOT) if args.baseline.is_absolute() and args.baseline.is_relative_to(ROOT) else args.baseline
        evidence["baseline"] = {"path": str(shown), "recorded": len(known), "new": new, "stale": stale,
                               "recorded_for": reviewed.get("commits", {})}
        evidence["incompatibilities"] = "\n".join("- " + line for line in reported)
        evidence["result"] = ("new incompatibilities require review" if new or stale
                              else f"no incompatibility beyond the {len(known)} recorded for this line")
        evidence["limits"] = ("exported type compatibility on this Go platform; excludes runtime, wire, "
                              "database and deployed-consumer behavior; the whole report stays in this JSON "
                              "beside the delta, which is why this gates without hiding anything")
        print(json.dumps(evidence, indent=2))
        for line in new:
            print("NEW  " + line, file=sys.stderr)
        for line in stale:
            print("GONE " + line, file=sys.stderr)
        return 1 if new or stale else 0
    evidence["result"] = "review required" if report else "no incompatibilities reported"
    evidence["limits"] = "exported type compatibility on this Go platform; excludes runtime, wire, database and deployed-consumer behavior"
    print(json.dumps(evidence, indent=2))
    return 1 if report else 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(2)
