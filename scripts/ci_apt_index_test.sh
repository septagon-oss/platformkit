#!/usr/bin/env bash
# A mirror that will not answer the index is tried again; a fact about the index is refused at once.
#
# Run 56143, job 56770 (head cea7e46b, 2026-10-09) turned this repository's `check` job red at
# `Install the database client, the socket probe and the YAML reader` with `##[error]Process completed
# with exit code 100`, and the whole of what that step had said was `E: Failed to fetch
# http://security.ubuntu.com/ubuntu/dists/noble-security/main/binary-amd64/Packages.gz  File has
# unexpected size (1377452 != 1380818). Mirror sync in progress?` followed by `E: Some index files
# failed to download.` The job's container had no `psql`, no `ss` and no `import yaml`, and the tree it
# was compiling had nothing to do with it.
#
# The two ways to get a retry wrong are both refused here: giving up on the first blip is what was
# red, and retrying forever is a hang a reader sees as a hung job rather than as a hung mirror. So the
# bound is the subject of these cases — which failures reach it, what they wait, how many tries it
# allows, and that a step which exhausts it ends non-zero having installed nothing.
#
# The classifier is asked directly, by sourcing scripts/ci_apt_index.sh in a subshell, so a failure
# text is answered in microseconds; the loop itself is run against a stub `apt-get` that writes down
# every call it receives. What those two cannot reach is that the workflows really call this: ci.yml's
# check job and mobile.yml's journey job each have their install step executed here, under the same
# `bash -e` and the same relative paths the job uses — the way scripts/ci_container_sweep_age_test.sh
# executes the sweep it guards. A step that went back to a bare `apt-get update` dies on the planted
# refusal in that run, rather than in a job page three hours later.
#
# The stub `python3`, `psql` and `ss` in that last group stand in for what the step installs: `make
# check` proves the real ones exist several lines before this one, and what is under test here is
# whether the step asks for them once apt has exited 0.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/ci-apt-index-XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
failures=0

check() { # check <case name> <want> <got>
	if [ "$2" = "$3" ]; then
		echo "ok   $1"
	else
		echo "FAIL: $1: got '$3', wanted '$2'"
		failures=$((failures + 1))
	fi
}

# The refusal the job died on, word for word from job 56770's log at 10:26:55Z.
SYNC='E: Failed to fetch http://security.ubuntu.com/ubuntu/dists/noble-security/main/binary-amd64/Packages.gz  File has unexpected size (1377452 != 1380818). Mirror sync in progress? [IP: 91.189.91.82 80]
   Release file created at: Fri, 09 Oct 2026 08:43:12 +0000
E: Some index files failed to download. They have been ignored, or old ones used instead.'

classification() { # classification <tries already spent> <what apt wrote> — "wait <seconds>" or "final"
	local after="$1" out="$2" pause
	(
		# shellcheck source=scripts/ci_apt_index.sh
		source "$root/scripts/ci_apt_index.sh"
		if pause="$(retry_wait "$after" "$out")"; then
			printf 'wait %s\n' "$pause"
		else
			printf 'final\n'
		fi
	)
}

expect() { # expect <want> <case name> <tries already spent> <what apt wrote>
	local want="$1" name="$2"
	if [ "$want" != "$(classification "$3" "$4")" ]; then
		echo "FAIL: $name: got '$(classification "$3" "$4")', wanted '$want'"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# 1..3. The blip the job died on is retried, the retry waits longer, and the bound is three tries.
expect "wait 10" "an index the mirror is still syncing is retried" 0 "$SYNC"
expect "wait 30" "the second blip waits the longer wait" 1 "$SYNC"
expect "final" "the third blip is the failure the run reports" 2 "$SYNC"

# 4..5. A fact about the index says nothing about the route to it and is reported on the first try.
expect "final" "a package the index does not carry is reported on the first try" 0 \
	"E: Unable to locate package iproute2"
expect "final" "unmet dependencies are reported on the first try" 0 \
	"E: Unmet dependencies. Try 'apt --fix-broken install' with no packages (or specify a solution)."

# 6..8. The same class as the sync, in the other three ways apt writes it.
expect "wait 10" "a fetch that timed out is retried" 0 \
	"Ign http://archive.ubuntu.com/ubuntu noble InRelease  Connection timed out [IP: 91.189.91.83 80]
E: Failed to fetch http://archive.ubuntu.com/ubuntu/dists/noble/InRelease  Connection timed out [IP: 91.189.91.83 80]"
expect "wait 10" "a name that did not resolve is retried" 0 \
	"Err:1 http://security.ubuntu.com/ubuntu noble-security InRelease
  Could not resolve host: security.ubuntu.com"
expect "wait 10" "a hash sum mismatch is retried" 0 \
	"W: Failed to fetch http://archive.ubuntu.com/ubuntu/dists/noble/main/binary-amd64/Packages.gz  Hash Sum mismatch"

# 9. The bound as data beside the bound as behaviour: the list the classifier above reads.
default_waits() {
	(
		# shellcheck source=scripts/ci_apt_index.sh
		source "$root/scripts/ci_apt_index.sh"
		printf '%s\n' "${apt_index_waits[*]}"
	)
}
check "the waits the step pays by default are 10s then 30s" "10 30" "$(default_waits)"

# The stub apt-get: it answers the two calls this repository's install step makes and records every
# one, so "nothing was installed" is a line of the log rather than an absence anybody infers.
mkdir -p "$scratch/bin"
cat >"$scratch/bin/apt-get" <<'STUB'
#!/usr/bin/env bash
# APT_STUB_UPDATE names one answer per `apt-get update` call — ok, sync or missing — and its last word
# answers every call past the end of the list. APT_STUB_INSTALL holds `ok` or nothing. The call is
# counted in a file beside the log rather than by reading the log back: PATH here holds no grep.
set -u
printf 'apt-get %s\n' "$*" >>"$APT_STUB_LOG"
count="${APT_STUB_LOG}.count"
ask=0
[ -f "$count" ] && read -r ask <"$count"
ask=$((ask + 1))
printf '%s\n' "$ask" >"$count"
answer() { # answer <word list> <which call, from one>
	local -a words
	read -r -a words <<<"$1"
	if [ "$2" -le "${#words[@]}" ]; then printf '%s' "${words[$((($2 - 1)))]}"; else printf '%s' "${words[-1]}"; fi
}
case "$1" in
update)
	case "$(answer "${APT_STUB_UPDATE:-ok}" "$ask")" in
	ok) printf 'Fetched 30.6 MB in 8s (3652 kB/s)\n' ;;
	sync) printf '%s\n' "${APT_STUB_SYNC:-E: Some index files failed to download.}" ; exit 100 ;;
	missing) printf 'E: Unable to locate package iproute2\n' ; exit 100 ;;
	*) exit 100 ;;
	esac
	;;
install)
	[ "${APT_STUB_INSTALL:-ok}" = ok ] || exit 100
	;;
*) exit 100 ;;
esac
STUB
chmod +x "$scratch/bin/apt-get"
ln -s "$(command -v bash)" "$scratch/bin/bash"
ln -s "$(command -v sleep)" "$scratch/bin/sleep"

run_index() { # run_index <answers> — scripts/ci_apt_index.sh against the stub, waits shortened
	: >"$scratch/log"
	rm -f "$scratch/log.count"
	local status=0
	APT_STUB_UPDATE="$1" APT_STUB_LOG="$scratch/log" APT_STUB_SYNC="$SYNC" PKIT_APT_INDEX_WAITS="0 0" \
		PATH="$scratch/bin" bash scripts/ci_apt_index.sh 2>&1 || status=$?
	return "$status"
}
update_calls() { grep -c '^apt-get update' "$scratch/log" || true; }
retry_lines() { grep -c '^+ retry ' <<<"$1" || true; }

# 10. An answering mirror is asked once, and its own words reach the job log rather than the retry
#     loop's stdout only.
out="$(run_index ok)" && got=0 || got=$?
check "an answering mirror exits 0" 0 "$got"
check "an answering mirror is asked once" 1 "$(update_calls)"
check "an answering mirror waits nothing" 0 "$(retry_lines "$out")"
check "an answering mirror's own words are in the log" "Fetched 30.6 MB in 8s (3652 kB/s)" "$(head -1 <<<"$out")"

# 11. The blip heals on the second ask: two asks, one retry line, and the run says which try it got.
out="$(run_index 'sync ok')" && got=0 || got=$?
check "a mirror that syncs in the meantime is asked again" 0 "$got"
check "a mirror that syncs in the meantime is asked twice" 2 "$(update_calls)"
check "a mirror that syncs in the meantime reports the retry it made" 1 "$(retry_lines "$out")"
check "a mirror that syncs in the meantime says which try answered" "apt index: fetched on try 2, after 1 retry" "$(tail -1 <<<"$out")"

# 12. The bound: three asks, two waits, then a refusal that says so. Nothing about the tree is inferred
#     from a fourth try that no code path can reach.
out="$(run_index sync)" && got=0 || got=$?
check "a mirror that never answers still refuses the step" 1 "$got"
check "a mirror that never answers is asked three times and no more" 3 "$(update_calls)"
check "a mirror that never answers pays both waits" 2 "$(retry_lines "$out")"
check "the refusal says what the run refused to wait out" "yes" \
	"$(grep -q '^apt index: refused on try 3 .*at most 3 times in all' <<<"$out" && echo yes || echo no)"

# 13. And a fact about the index is reported with no wait and no second ask, which is the difference
#     between this retry and a `|| true`.
out="$(run_index missing)" && got=0 || got=$?
check "an index that does not carry the package refuses the step" 1 "$got"
check "an index that does not carry the package is asked once" 1 "$(update_calls)"
check "an index that does not carry the package waits nothing" 0 "$(retry_lines "$out")"
check "an index that does not carry the package says so in apt's words" 1 "$(grep -c 'E: Unable to locate package iproute2' <<<"$out")"

# 14. The first real wait, paid. The cases above shorten the list to run the bound; this one runs the
#     list the step actually pays, so the number is a waited second and not only a printed one.
start=$SECONDS
: >"$scratch/log"
rm -f "$scratch/log.count"
APT_STUB_UPDATE='sync ok' APT_STUB_LOG="$scratch/log" APT_STUB_SYNC="$SYNC" PATH="$scratch/bin" \
	bash scripts/ci_apt_index.sh >/dev/null 2>&1 || true
paid=$((SECONDS - start))
if [ "$paid" -ge 10 ]; then
	echo "ok   the first wait the step pays is the ${paid}s it says it pays"
else
	echo "FAIL: the first real wait came back in ${paid}s; the step waits 10s before its second ask"
	failures=$((failures + 1))
fi

# Every step in the program's workflows that asks apt for anything, run as the job runs it: same
# `bash -e`, same relative paths, PATH the stub directory and nothing else. Its own tools are stubbed
# beside apt's, so a package that installs nothing is refused rather than green.
printf '#!/bin/sh\nexit 0\n' >"$scratch/bin/python3"
printf '#!/bin/sh\nexit 0\n' >"$scratch/bin/psql"
printf '#!/bin/sh\nexit 0\n' >"$scratch/bin/ss"
chmod +x "$scratch/bin/python3" "$scratch/bin/psql" "$scratch/bin/ss"

PYTHONDONTWRITEBYTECODE=1 python3 - "$root" "$scratch" "$SYNC" <<'PY'
import glob, os, subprocess, sys, yaml

root, scratch, sync = sys.argv[1], sys.argv[2], sys.argv[3]
failures = 0


def case(name, want, got):
    global failures
    if want == got:
        print(f"ok   {name}")
    else:
        print(f"FAIL: {name}: got {got!r}, wanted {want!r}")
        failures += 1


# Every step that goes through the retry, across every workflow, with the checkout it depends on.
retry_steps, direct = [], []
for path in sorted(glob.glob(os.path.join(root, ".gitea", "workflows", "*.yml"))):
    name = os.path.basename(path)
    for job, body in ((yaml.safe_load(open(path)) or {}).get("jobs") or {}).items():
        steps = body.get("steps") or []
        for i, step in enumerate(steps):
            text = step.get("run") or ""
            if "bash scripts/ci_apt_index.sh" in text:
                checkout = next((j for j, s in enumerate(steps[:i])
                                 if str(s.get("uses") or "").startswith("actions/checkout@")), None)
                retry_steps.append((f"{name} job {job}", step, checkout is not None))
            for line in text.splitlines():
                if line.strip().startswith("apt-get update"):
                    direct.append(f"{name}:{job}")

if not retry_steps:
    print("FAIL: no step of any workflow under .gitea/workflows calls scripts/ci_apt_index.sh")
    sys.exit(1)


def run(step, update, tools=("python3", "psql", "ss")):
    """One committed step body under bash -e; `tools` are the stubs allowed to answer."""
    env = {k: str(v) for k, v in (step.get("env") or {}).items() if "${{" not in str(v)}
    env.update(dict(os.environ, PATH=os.path.join(scratch, "bin"),
                    APT_STUB_LOG=os.path.join(scratch, "log"), APT_STUB_UPDATE=update,
                    APT_STUB_SYNC=sync, PKIT_APT_INDEX_WAITS="0 0"))
    open(env["APT_STUB_LOG"], "w").close()
    try:
        os.remove(env["APT_STUB_LOG"] + ".count")
    except FileNotFoundError:
        pass
    hidden = [tool for tool in ("python3", "psql", "ss") if tool not in tools]
    for tool in hidden:
        os.rename(os.path.join(scratch, "bin", tool), os.path.join(scratch, "bin", tool + ".held"))
    result = subprocess.run(["bash", "-e", "-c", step["run"]], cwd=root, env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=120)
    for tool in hidden:
        os.rename(os.path.join(scratch, "bin", tool + ".held"), os.path.join(scratch, "bin", tool))
    return result, open(env["APT_STUB_LOG"]).read().splitlines()


for label, step, checked_out in retry_steps:
    installs = [line.strip() for line in (step.get("run") or "").splitlines()
                if line.strip().startswith("apt-get install ")]
    case(f"{label}: its checkout runs before the step that calls the retry", True, checked_out)
    result, log = run(step, "sync ok")
    # A bare `apt-get update` in this step dies on the first sync refusal, because the job runs it under
    # `bash -e`. Only a step that goes through the retry survives it and installs at all.
    case(f"{label}: the install step outlasts one mirror sync", 0, result.returncode)
    case(f"{label}: the index is asked twice before anything installs", 2,
         len([line for line in log if line == "apt-get update"]))
    case(f"{label}: its own package line is installed once", installs,
         [line for line in log if line.startswith("apt-get install")])
    result, log = run(step, "sync")
    # A mirror that never answers leaves nothing installed: the step refuses with the index unfetched,
    # and no package is pulled against lists apt said it could not read.
    case(f"{label}: a mirror that never answers refuses the step", True, result.returncode != 0)
    case(f"{label}: nothing installs against unreadable lists", [],
         [line for line in log if line.startswith("apt-get install")])

# The one step that buys three tools asks all three whether they arrived.
three = [s for s in retry_steps if "missing=" in (s[1].get("run") or "")][0]
result, log = run(three[1], "ok", tools=("python3", "psql"))
case("the install step refuses when the socket probe is missing", True, result.returncode != 0)
case("the refusal names the tool it is missing", True, " ss" in result.stderr)
result, log = run(three[1], "ok", tools=("ss", "psql"))
case("the install step refuses when the YAML reader is missing", True, result.returncode != 0)
case("that refusal names the reader it is missing", True, "python3-yaml" in result.stderr)
result, log = run(three[1], "ok")
case("the install step passes when all three answer", (0, ""), (result.returncode, result.stderr.strip()))

# The held exception, named. public-consumption.yml's report job still runs `apt-get update` itself, on
# a host this file has never seen a log from: that job has no container of its own, and whether its
# runner even carries apt is a question about a machine rather than about this tree, so the retry is not
# offered there on evidence. Curing it, or a new direct `apt-get update` anywhere, fails here and asks
# for this list to change with a reason beside it.
case("the only step left asking apt-get update directly is public-consumption.yml's report job",
     ["public-consumption.yml:report"], direct)

sys.exit(1 if failures else 0)
PY

if grep -q 'bash scripts/ci_apt_index_test.sh' "$root/Makefile"; then
	echo "ok   make check runs scripts/ci_apt_index_test.sh"
else
	echo "FAIL: Makefile's check recipe does not run this file, so nothing runs it" >&2
	failures=$((failures + 1))
fi

if [ "$failures" -ne 0 ]; then
	echo "ci_apt_index: $failures case(s) failed" >&2
	exit 1
fi
echo "ci_apt_index: a mirror that will not answer the index is asked three times over 40 s and then refused, a fact about the index is refused at once, and every workflow step that asks apt for a package goes through that bound"
