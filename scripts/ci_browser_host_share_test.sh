#!/usr/bin/env bash
# The three browser suites of .gitea/workflows/ci.yml ask the host for a core, print what they waited
# through, and hand their own exit status back. Every case writes only into one temporary directory.
#
# Why these questions. The `design` job's browser step refuses with a browser death — a `Target
# crashed` in a case that measures 1.7 s when anybody runs it by hand — and the step that refused had
# no idea what the host was doing or how full the buffer pool its renderer died in was.
# scripts/ci_browser_host_share.sh is the answer to both, and a guard nobody runs is prose, so one
# group of cases runs its three subcommands: a host with a share to give starts at once, a host
# without one waits — including one whose last minute dipped while its last five stayed queued over
# one task per core — the bound ends the wait rather than the run, a host that cannot be read says so,
# one sample names the memory, the queue, the ceiling and the pool, and the suite's own verdict
# survives the sampler wrapped around it. The container's ceiling is asked of all three shapes a host
# can leave memory accounting in — cgroup v2, cgroup v1, and none — because a promise one layout keeps
# and another breaks is a promise about the image, not about the run. The rest ask what the guard
# itself cannot: whether the three browser steps of the workflow still call it and reach it from the
# directory they run in, and whether this file would notice one that stopped — six planted refusals
# over copied workflow files, the design step's two, its own reach, and gate 10's three, and a copied
# Makefile that lost either of the two lines that run these
# cases.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
guard="$root/scripts/ci_browser_host_share.sh"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }

# A host with a free share: the suite starts, and the answer is immediate because a step that
# waited for nothing would be charged for it at every run.
printf '2.0 3.0 4.0 1/200 99\n' > "$temporary/quiet"
out="$(bash "$guard" wait 60 "$temporary/quiet" 1)" || fail "wait refused a host with a free share"
case "$out" in
	*"the host has a free share"*"load 2.0 3.0 4.0"*) ;;
	*) fail "wait did not say which host it accepted: $out" ;;
esac

# A host whose run queue is two deep per core: the wait is what the step does, and the bound is what
# ends it. The bound answers 0 rather than refusing, so the case asks for both sentences.
printf '9999.0 9999.0 9999.0 1/200 99\n' > "$temporary/busy"
out="$(bash "$guard" wait 1 "$temporary/busy" 1)" || fail "a host with no free share failed the step"
case "$out" in
	*"waiting for a free core"*"still has no free share after"*) ;;
	*) fail "the wait neither waited nor said it was starting anyway: $out" ;;
esac

# A host whose last minute dipped while its last five minutes stayed queued. The first term alone is
# what opened one gate-10 suite on `load 12.98 16.23 19.83 across 16 cores` and lost a page at 21.74
# four minutes in (job 80264 at 5b0dc4c0), so the 5-minute term is asked beside it and the wait — not
# the browser — is what the step does on a host still working its queue off.
printf '1.0 9999.0 9999.0 1/200 99\n' > "$temporary/dipped"
out="$(bash "$guard" wait 1 "$temporary/dipped" 1)" || fail "a host with no free share over five minutes failed the step"
case "$out" in
	*"waiting for a free core"*) ;;
	*) fail "wait read a dipped minute on a queued host as a free share: $out" ;;
esac

# The wait ends when the host frees up: the same file answers busy and then quiet, and the step is
# expected to say how long it stood. A background writer, not a server — it runs once and exits.
printf '9999.0 9999.0 9999.0 1/200 99\n' > "$temporary/frees"
( sleep 0.6; printf '1.0 1.0 1.0 1/200 99\n' > "$temporary/frees" ) &
writer=$!
out="$(bash "$guard" wait 30 "$temporary/frees" 1)" || fail "the wait did not end when the host freed up"
wait "$writer"
case "$out" in
	*"waiting for a free core"*"free share after"*) ;;
	*) fail "the wait did not report the queue it waited through and then saw clear: $out" ;;
esac

# A host that cannot be read is a runner without /proc, and the step still has to run: the sentence
# is the whole point, because silence here is indistinguishable from a host that was idle.
out="$(bash "$guard" wait 5 "$temporary/absent" 1)" || fail "an unreadable load average failed the step"
case "$out" in *"no readable load average"*) ;; *) fail "an unreadable load average printed no sentence: $out" ;; esac

# One sample names the three things a later log will be read for, and a source that is not there
# prints a sentence instead of nothing.
printf 'MemTotal:       1000000 kB\nMemAvailable:     512000 kB\n' > "$temporary/meminfo"
out="$(bash "$guard" report "$temporary/meminfo")" || fail "report refused a readable meminfo"
for line in 'runner MemAvailable 500 MB' 'runner load average' '/dev/shm' 'memory ceiling'; do
	case "$out" in *"$line"*) ;; *) fail "report printed no [$line]: $out" ;; esac
done
out="$(bash "$guard" report "$temporary/not-a-meminfo")" || fail "report refused a meminfo it could not read"
case "$out" in *"no readable $temporary/not-a-meminfo"*) ;; *) fail "an unreadable meminfo printed silence: $out" ;; esac

# The container's own memory accounting, in the three shapes a host can leave it in. The guard asks
# fixed paths under /sys, which no case can plant, so the guard is copied with its accounting
# directory moved into this file's own — and the directory it moves is read out of the guard rather
# than named here, so a guard that moves its paths keeps being tested against the paths it asks. The
# planted directory is named `cgroup` at the end, as the real one is, because the guard shortens each
# counter's name at that word and the fixture has to be reported the way a real host is. What all
# three shapes owe is the same sentence: a readable accounting prints its counters under a line that
# names the ceiling they were read from, and a host that exposes none says so — the ceiling is the
# question this block exists to answer, and an answer that depends on the layout would leave the log
# silent on exactly the hosts whose deaths it was written to attribute.
cgroup_dir() { # the directory the guard reads its memory accounting from, as the guard itself says it
	sed -n 's#.*for file in \([^ ]*\)/memory\.max .*#\1#p' "$guard" | head -1
}
accounting_dir="$(cgroup_dir)"
[ -n "$accounting_dir" ] || fail 'the guard no longer names a memory accounting directory this case can copy'
plant_accounting() { # <layout> <file> <content>...: a copied guard answering to a directory of ours
	local layout="$1" dir file content
	dir="$temporary/cgroup-$layout/cgroup"
	shift
	mkdir -p "$dir"
	while [ "$#" -gt 0 ]; do
		file="$1" content="$2"
		shift 2
		mkdir -p "$dir/$(dirname "$file")"
		printf '%s\n' "$content" > "$dir/$file"
	done
	sed "s|$accounting_dir/|$dir/|g" "$guard" > "$temporary/guard-$layout.sh"
}
require_line() { # <who answered> <needle> <the sample>: a needle the sample must carry
	case "$3" in
		*"$2"*) ;;
		*) fail "$1 printed no [$2]: $3" ;;
	esac
}
plant_accounting v2 memory.max 1073741824 memory.current 67108864 memory.events 'low 0 high 0 max 0 oom 0 oom_kill 0'
out="$(bash "$temporary/guard-v2.sh" report "$temporary/meminfo")" || fail 'report refused a cgroup v2 host'
for line in 'memory ceiling' 'memory.max 1073741824' 'memory.current 67108864' 'memory.events low 0 high 0 max 0 oom 0 oom_kill 0'; do
	require_line 'a cgroup v2 host' "$line" "$out"
done
case "$out" in *'no readable memory ceiling'*) fail "a cgroup v2 host with a readable ceiling was reported as having none: $out" ;; esac
plant_accounting v1 memory/memory.limit_in_bytes 1073741824 memory/memory.usage_in_bytes 67108864 memory/memory.failcnt 0
out="$(bash "$temporary/guard-v1.sh" report "$temporary/meminfo")" || fail 'report refused a cgroup v1 host'
for line in 'memory ceiling' 'memory/memory.limit_in_bytes 1073741824' 'memory/memory.usage_in_bytes 67108864' 'memory/memory.failcnt 0'; do
	require_line 'a cgroup v1 host' "$line" "$out"
done
# A ceiling read is never reported as missing: the sentence belongs to the host that had nothing to
# read, and a sample answering the ceiling question two ways at all says nothing about either.
case "$out" in *'no readable memory ceiling'*) fail "a cgroup v1 host with a readable ceiling was reported as having none: $out" ;; esac
plant_accounting absent
out="$(bash "$temporary/guard-absent.sh" report "$temporary/meminfo")" || fail 'report refused a host with no memory accounting'
case "$out" in
	*'no readable memory ceiling'*) ;;
	*) fail 'a host with no memory accounting printed no sentence about its ceiling' ;;
esac

# The suite's verdict survives the sampler wrapped around it — the one way this cure could have been
# a weakening, and the reason the case asks for a failure and not only a pass.
out="$(bash "$guard" watch 1 sh -c 'exit 3')" || rc=$?
rc="${rc:-0}"
[ "$rc" = 3 ] || fail "a suite that failed answered $rc instead of 3: $out"
case "$out" in *"runner MemAvailable"*) ;; *) fail "watch sampled nothing while the suite ran: $out" ;; esac
# The first sample belongs to the step, not to the sampler behind it, and the order is the whole
# difference: a sampler left to take the first reading can be killed before the scheduler gives it a
# turn, which leaves a quick run with no number at all. The interval is 60 so the only sample this
# case can see is the one taken before COMMAND starts, and the suite's own line has to arrive last.
out="$(bash "$guard" watch 60 sh -c 'echo the-suite-spoke')" || fail "watch refused a suite that only spoke"
case "$out" in *"the-suite-spoke"*) ;; *) fail "watch ran no suite at all: $out" ;; esac
case "$out" in "the-suite-spoke"*) fail "watch ran the suite before its first sample: $out" ;; esac
out="$(bash "$guard" watch 1 sh -c 'sleep 1')" || fail "a suite that passed failed the step"
case "$out" in *"peaked at"*"MB used"*) ;; *) fail "watch printed no peak for the pool it was watching: $out" ;; esac

# A call that names nothing to watch is a step that would run an empty suite.
bash "$guard" watch 1 >/dev/null 2>&1 && fail "watch accepted no command" || true
bash "$guard" >/dev/null 2>&1 && fail "the guard answered no subcommand" || true

# The three browser steps of the real workflow, and one that lost the guard. The steps are read as
# data, the way scripts/ci_go_cache_test.sh reads the cache steps: the question is which commands a
# job will run, and a grep over the file answers it for text that has been renamed or reflowed away.
steps_call_guard() { # <workflow file>: one line per browser step, ok or the reason it does not
	python3 - "$1" <<'PY'
import re, sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    jobs = (yaml.safe_load(handle) or {}).get("jobs") or {}
wanted = {
    "check": ("name", "make e2e", "bash scripts/ci_browser_host_share.sh"),
    "design": ("name", "Native browser observations", "bash ../../../scripts/ci_browser_host_share.sh"),
    "editor": ("run", "editor/*.test.mjs", "bash ../../../scripts/ci_browser_host_share.sh"),
}
for job, (field, needle, path) in wanted.items():
    steps = [s for s in ((jobs.get(job) or {}).get("steps") or []) if s.get("run")]
    hit = next((s for s in steps if needle in str(s.get(field) or "")), None)
    if hit is None:
        print(f"{job}: no step whose {field} names {needle}")
        continue
    run = str(hit.get("run"))
    # The path is part of the guard, and it is asked of each step's own working directory: the two
    # design-tool suites resolve the guard three levels up, gate 10 from the repository root. Either
    # answer written in the wrong step names a file that is not there, and that step dies before its
    # first browser rather than guarding anything.
    calls = set()
    for line in run.splitlines():
        line = line.strip()
        # A `#` line of a run block is the step's own prose, not a command it will exec, and those
        # prose lines name the guard file as often as the two lines that call it.
        if "ci_browser_host_share.sh" not in line or line.startswith("#"):
            continue
        calls.add(line[:line.index("ci_browser_host_share.sh") + len("ci_browser_host_share.sh")])
    if not calls:
        print(f"{job}: its browser suite calls no host-share guard")
        continue
    if calls != {path}:
        print(f"{job}: its browser suite does not reach the guard from the directory it runs in: {sorted(calls)}")
        continue
    waits = re.findall(r"ci_browser_host_share\.sh wait (\d+)", run)
    watches = re.findall(r"ci_browser_host_share\.sh watch (\d+)", run)
    if len(waits) != 1 or len(watches) != 1:
        print(f"{job}: the browser suite is not guarded once each — wait {waits}, watch {watches}")
        continue
    print(f"{job}: ok wait={waits[0]} watch={watches[0]}")
PY
}
verdict="$(steps_call_guard "$root/.gitea/workflows/ci.yml")" || fail "the workflow could not be read as data"
echo "$verdict" | grep -qx 'design: ok wait=300 watch=30' || fail "the design job's browser suite is not guarded:
$verdict"
echo "$verdict" | grep -qx 'editor: ok wait=720 watch=30' || fail "the editor job's browser suite is not guarded:
$verdict"
echo "$verdict" | grep -qx 'check: ok wait=600 watch=30' || fail "gate 10's browser suite is not guarded:
$verdict"

# The same assertion over a workflow with the design step's guard deleted: red here means the case
# above would notice a step that quietly stopped asking.
mutated="$temporary/ci-missing-guard.yml"
sed 's#scripts/ci_browser_host_share\.sh wait 300#missing_host_share_guard wait 300#' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -qx 'design: ok wait=300 watch=30'; then
	fail "the workflow case accepted a design step with no wait before its first browser"
fi
mutated="$temporary/ci-half-guard.yml"
sed 's#bash ../../../scripts/ci_browser_host_share\.sh watch 30 npm run test:browser#npm run test:browser#' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -qx 'design: ok wait=300 watch=30'; then
	fail "the workflow case accepted a design suite that runs unwatched"
fi
# The same guard reached from the wrong directory: a step that calls `scripts/…` while the design tool
# is its working directory names a file that is not there, and the step dies before its first browser
# rather than guarding anything.
mutated="$temporary/ci-rooted-path.yml"
sed 's#bash \.\./\.\./\.\./scripts/ci_browser_host_share\.sh#bash scripts/ci_browser_host_share.sh#g' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -q 'design: ok'; then
	fail "the workflow case accepted a guard the step's own working directory cannot reach"
fi

# Gate 10's own three, the same three questions over the suite whose death is the reason it was
# asked: a step that lost its wait, a suite the sampler no longer wraps, and a guard named from a
# directory too deep for the step that calls it. Each is refused for gate 10 rather than for the two
# suites guarded first, so a workflow that lost gate 10 alone cannot pass on their answers.
mutated="$temporary/ci-gate10-lost-wait.yml"
sed 's#^\( *\)bash scripts/ci_browser_host_share\.sh wait 600#\1bash missing_gate10_guard wait 600#' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -qx 'check: ok wait=600 watch=30'; then
	fail "the workflow case accepted gate 10 starting a Chromium with no wait before its first page"
fi
mutated="$temporary/ci-gate10-unwatched.yml"
sed 's#^\( *\)bash scripts/ci_browser_host_share\.sh watch 30 make e2e#\1make e2e#' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -qx 'check: ok wait=600 watch=30'; then
	fail "the workflow case accepted a gate 10 suite that runs unwatched"
fi
mutated="$temporary/ci-gate10-deep-path.yml"
sed 's#^\( *\)bash scripts/ci_browser_host_share\.sh#\1bash ../../../scripts/ci_browser_host_share.sh#' \
	"$root/.gitea/workflows/ci.yml" > "$mutated"
if steps_call_guard "$mutated" | grep -qx 'check: ok wait=600 watch=30'; then
	fail "the workflow case accepted a guard gate 10's own working directory cannot reach"
fi

echo "ok   a browser suite waits for a core, prints the host it waited for, and keeps its own verdict"

# And the two lines that run these cases. A recipe that leaves `check` leaves nothing else red — `go tool
# gotestsum --packages='./...'` sees no shell script and no python file — so the case asks the dry run
# for each command, and then asks a Makefile with that one line deleted the same question. The nested
# `make -n` against a tree holding nothing but a go.mod is the shape scripts/check_architecture_test.sh
# already runs from inside this same recipe; the four together cost one second.
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p; /^toolchain[[:space:]]/p' "$root/go.mod" > "$temporary/go.mod"
check_recipe_runs_the_case() { # <makefile> <the line the check goal must still run>
	local recipe="$temporary/recipe.txt"
	make --no-print-directory -n -C "$temporary" -f "$1" check > "$recipe" 2>/dev/null || true
	grep -qx "$2" "$recipe"
}
for recipe_line in 'bash scripts/ci_browser_host_share_test.sh' \
                   'PYTHONDONTWRITEBYTECODE=1 python3 scripts/ci_browser_guard_readable_cgroup_test.py'; do
	check_recipe_runs_the_case "$root/Makefile" "$recipe_line" || fail "make check no longer runs $recipe_line"
	awk -v line="$recipe_line" 'index($0, "\t" line) != 1' "$root/Makefile" > "$temporary/Makefile-lost-line"
	if check_recipe_runs_the_case "$temporary/Makefile-lost-line" "$recipe_line"; then
		fail "the recipe case accepted a check goal that dropped $recipe_line"
	fi
done
echo "ok   make check runs both lines that run these cases, and a recipe that lost either is refused"
