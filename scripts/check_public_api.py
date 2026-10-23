#!/usr/bin/env python3
"""Report exported API incompatibilities between two committed source revisions.

Within a release line the exported surface is frozen at one revision, named by the
line record given to --line-record, and any reported incompatibility is a refusal:
see docs/COMPATIBILITY.md. A base revision given on the command line instead compares
two published revisions, which is the release review's question rather than a gate.
"""
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
# when it cannot write its report), and this gate's verdict is the report below, which
# no retry touches. Any other non-zero exit is reported on the first try.
FETCH_MARKERS = ("loading deprecation", "dial tcp", "no such host", "i/o timeout",
                 "connection refused", "server misbehaving", "bad gateway",
                 "service unavailable", "gateway time-out", "unexpected eof",
                 "eof while", "tls:", "context deadline exceeded")
# One wait per extra try: two seconds of retrying an eight-second lookup buys about
# nothing, so the second wait is long enough to sit out a resolver blip of the size the
# loop's own logs show, and the whole retry is still well inside the minute this check
# costs when the network is answering.
FETCH_WAITS = (5, 20)

# What the verdict does not know, measured rather than assumed: a name absent at the
# compared revision is reported only as `added`, so its shape is not compared, and
# apidiff reads Go source, so a JSON tag, a wire field or a CLI flag carries no line
# here at all. docs/COMPATIBILITY.md names the mechanism that refuses each of those.
LIMITS = ("exported Go type compatibility on this platform, read at the exported surface of the "
          "compared revision; a name absent there is reported only as added, so its shape is not "
          "compared; JSON struct tags, served wire fields and CLI flags carry no line here; internal "
          "packages, runtime behavior and deployed-consumer behavior are excluded")

JUDGED_FROZEN = "the frozen surface"
JUDGED_DELTA = "this change's own delta"


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


def parse_arguments(argv):
    """The revisions to compare, and the line record when one names the base.

    `--line-record` resolves the base itself, so nothing on the command line can name a
    tag that has not been published yet; the revision it leaves is the candidate.
    Without it both revisions are named by the caller, which is the release review's
    shape. A base revision beside `--line-record` names two answers to one question and
    is refused.
    """
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("revisions", nargs="*", metavar="REVISION",
                        help="the supported tag or commit, then the candidate; the candidate defaults to HEAD")
    parser.add_argument("--line-record", type=Path,
                        help="the release line's record, whose published tag or frozen commit is the base; docs/COMPATIBILITY.md")
    args = parser.parse_args(argv)
    if args.line_record:
        if len(args.revisions) > 1:
            parser.error("--line-record names the base, so name at most the candidate revision")
        args.base, args.candidate = None, (args.revisions[0] if args.revisions else "HEAD")
    elif len(args.revisions) == 1:
        args.base, args.candidate = args.revisions[0], "HEAD"
    elif len(args.revisions) == 2:
        args.base, args.candidate = args.revisions
    else:
        parser.error("name a base revision, or pass --line-record")
    return args


def source_toolchain():
    """The `go` binary and environment the source go.mod directive selects."""
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
    return go, env, go_version


def command_runner(env, evidence):
    """Build the one command runner this gate uses, which records every try it made.

    Every try is the same command in the same directory against the same pinned tool;
    only whether the proxy answered differs. `exit_code` and `fetch_retries` in the
    evidence are the difference between a quiet run and one that got there on the third
    attempt, which is what a reader needs to weigh the result later.
    """
    def run(command, cwd=None, allow=(0,)):
        waits = 0
        while True:
            result = subprocess.run(command, cwd=cwd, env=env, text=True,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=300)
            refused = (result.stdout + result.stderr).strip()
            if result.returncode in allow:
                evidence["commands"].append({"arguments": command, "exit_code": result.returncode,
                                             "fetch_retries": waits})
                return result.stdout
            wait = fetch_wait(waits, refused)
            evidence["commands"].append({"arguments": command, "exit_code": result.returncode,
                                         "fetch_retries": waits})
            if wait is None:
                raise RuntimeError(refused or " ".join(command) + " exited " + str(result.returncode))
            waits += 1
            print(f"+ retry {waits} after a module fetch that did not answer, waiting {wait}s: "
                  f"{refused.splitlines()[-1] if refused else 'no output'}",
                  file=sys.stderr, flush=True)
            time.sleep(wait)
    return run


def commit_of(run, revision):
    """The full commit digest a revision names, with `^{commit}` so a tag resolves to its commit."""
    return run(["git", "rev-parse", "--verify", "--end-of-options", revision + "^{commit}"],
               ROOT).strip()


def line_record(run, path):
    """The release line's own record of what it froze, as the evidence prints it.

    `line` names the tag that, once published, is the base; `frozen.commit` is the
    revision the line froze until that tag exists. A record that names neither is a gate
    with no question, so it is refused here, at the file that failed, rather than by
    comparing against something arbitrary.
    """
    record = json.loads(path.read_text())
    tag = str(record.get("line") or "").strip()
    shown = path.relative_to(ROOT) if path.is_absolute() and path.is_relative_to(ROOT) else path
    tag_commit = ""
    if tag:
        tag_commit = run(["git", "rev-parse", "-q", "--verify", "refs/tags/" + tag + "^{commit}"],
                         ROOT, allow=(0, 1)).strip()
    wanted = tag_commit or str(record.get("frozen", {}).get("commit") or "").strip()
    if not wanted:
        raise RuntimeError(f"{shown} names neither a published tag nor a frozen commit")
    return {"record": str(shown), "tag": tag, "tag_exists": bool(tag_commit), "wanted": wanted}


def exported_surface(run, go, revision, label, destination):
    """Write the pinned tool's export set for one committed revision under `destination`."""
    commit = commit_of(run, revision)
    source = destination / label
    source.mkdir()
    data = subprocess.check_output(["git", "archive", commit], cwd=ROOT)
    with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as archive:
        archive.extractall(source, filter="data")
    declaration = json.loads(run([go, "mod", "edit", "-json"], source))
    if declaration.get("Replace"):
        raise RuntimeError("release API snapshots must not use local or module replacements")
    print("+ export " + label + " " + commit, file=sys.stderr, flush=True)
    run([go, "run", TOOL, "-m", "-w", str(destination / (label + ".export")),
         declaration["Module"]["Path"]], source)
    return commit


def published_report(run, go, temporary):
    """Every incompatibility apidiff reports, one per line, internal packages dropped."""
    output = run([go, "run", TOOL, "-m", "-incompatible", str(temporary / "base.export"),
                  str(temporary / "candidate.export")], temporary)
    return [line[2:] if line.startswith("- ") else line
            for line in output.splitlines() if line and not line.startswith("Ignoring internal package ")]


def verdict(evidence, reported, frozen):
    """Print the evidence and one BREAK line per reported incompatibility; return the exit code.

    Any reported line is a refusal: a base revision and no list of accepted changes is
    the freeze, so a break somebody wrote down refuses the run exactly like one nobody
    did. The evidence keeps the whole report rather than a delta, and `frozen` says which
    revision was asked about.
    """
    evidence["incompatibilities"] = "\n".join("- " + sentence for sentence in reported)
    evidence["limits"] = LIMITS
    if frozen:
        evidence["result"] = (f"the exported API changed on a frozen line: {len(reported)}" if reported
                              else "no exported incompatibility with the frozen line")
    else:
        evidence["result"] = "review required" if reported else "no incompatibilities reported"
    print(json.dumps(evidence, indent=2))
    for sentence in reported:
        print("BREAK " + sentence, file=sys.stderr)
    return 1 if reported else 0


def main():
    args = parse_arguments(sys.argv[1:])
    go, env, go_version = source_toolchain()
    evidence = {"tool": TOOL, "go_version": go_version, "commits": {}, "commands": []}
    run = command_runner(env, evidence)
    candidate = commit_of(run, args.candidate)
    if args.line_record:
        line = line_record(run, args.line_record)
        # A branch cut before the freeze is judged on its own delta: against the freeze
        # itself it would be refused for the change the freeze already carries. Main is
        # judged on the frozen surface, because the freeze is its merge base. The
        # evidence says which of the two questions this run asked.
        used = run(["git", "merge-base", line["wanted"], candidate], ROOT).strip()
        line["used"] = used
        line["judged"] = JUDGED_FROZEN if used == line["wanted"] else JUDGED_DELTA
        base, revisions = used, {"base": used, "candidate": candidate}
        print(f"line {line['tag']}: exported surface frozen at {line['wanted'][:12]} "
              f"(judged by {line['judged']})", file=sys.stderr, flush=True)
    else:
        line, revisions = None, {"base": args.base, "candidate": args.candidate}
    with tempfile.TemporaryDirectory(prefix="platformkit-api-check-") as directory:
        temporary = Path(directory)
        for label, revision in revisions.items():
            evidence["commits"][label] = exported_surface(run, go, revision, label, temporary)
        evidence["temporary_fixture_removed"] = True
        if line:
            evidence["line"] = line
        reported = published_report(run, go, temporary)
    return verdict(evidence, reported, line)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(2)
