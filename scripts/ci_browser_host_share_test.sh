#!/usr/bin/env bash
# The browser suites of .gitea/workflows/ci.yml ask the host for a core, print what they waited
# through, and hand their own exit status back. Every case writes only into one temporary directory.
#
# Why these questions. The `design` job's browser step refuses with a browser death — a `Target
# crashed` in a case that measures 1.7 s when anybody runs it by hand — and the step that refused had
# no idea what the host was doing or how full the buffer pool its renderer died in was.
# scripts/ci_browser_host_share.sh is the answer to both, and a guard nobody runs is prose, so one
# group of cases runs its three subcommands: a host with a share to give starts at once, a host
# without one waits, the bound ends the wait rather than the run, a host that cannot be read says so,
# one sample names the memory, the queue, the ceiling and the pool, and the suite's own verdict
# survives the sampler wrapped around it. The rest ask what the guard itself cannot: whether the two
# browser steps of the workflow still call it and reach it from the directory they run in, and whether
# this file would notice one that stopped — three planted refusals over copied workflow files, and a
# copied Makefile that lost the line that runs this one.
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

# The suite's verdict survives the sampler wrapped around it — the one way this cure could have been
# a weakening, and the reason the case asks for a failure and not only a pass.
out="$(bash "$guard" watch 1 sh -c 'exit 3')" || rc=$?
rc="${rc:-0}"
[ "$rc" = 3 ] || fail "a suite that failed answered $rc instead of 3: $out"
case "$out" in *"runner MemAvailable"*) ;; *) fail "watch sampled nothing while the suite ran: $out" ;; esac
out="$(bash "$guard" watch 1 sh -c 'sleep 1')" || fail "a suite that passed failed the step"
case "$out" in *"peaked at"*"MB used"*) ;; *) fail "watch printed no peak for the pool it was watching: $out" ;; esac

# A call that names nothing to watch is a step that would run an empty suite.
bash "$guard" watch 1 >/dev/null 2>&1 && fail "watch accepted no command" || true
bash "$guard" >/dev/null 2>&1 && fail "the guard answered no subcommand" || true

# Both browser steps of the real workflow, and one that lost the guard. The steps are read as data,
# the way scripts/ci_go_cache_test.sh reads the cache steps: the question is which commands a job
# will run, and a grep over the file answers it for text that has been renamed or reflowed away.
steps_call_guard() { # <workflow file>: one line per browser step, ok or the reason it does not
	python3 - "$1" <<'PY'
import re, sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    jobs = (yaml.safe_load(handle) or {}).get("jobs") or {}
wanted = {"design": ("name", "Native browser observations"), "editor": ("run", "editor/*.test.mjs")}
for job, (field, needle) in wanted.items():
    steps = [s for s in ((jobs.get(job) or {}).get("steps") or []) if s.get("run")]
    hit = next((s for s in steps if needle in str(s.get(field) or "")), None)
    if hit is None:
        print(f"{job}: no step whose {field} names {needle}")
        continue
    run = str(hit.get("run"))
    # The path is part of the guard: both steps run with the design tool as their working directory,
    # so a call that names `scripts/` alone would resolve to a file that does not exist there.
    if "bash ../../../scripts/ci_browser_host_share.sh" not in run:
        print(f"{job}: its browser suite does not reach the guard from the directory it runs in")
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

echo "ok   a browser suite waits for a core, prints the host it waited for, and keeps its own verdict"

# And the line that runs this file. A recipe that leaves `check` leaves nothing else red — `go tool
# gotestsum --packages='./...'` sees no shell script — so the case asks the dry run for its own
# command, and then asks a Makefile with that one line deleted the same question. The nested `make -n`
# against a tree holding nothing but a go.mod is the shape scripts/check_architecture_test.sh already
# runs from inside this same recipe; the two together cost half a second.
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p; /^toolchain[[:space:]]/p' "$root/go.mod" > "$temporary/go.mod"
check_recipe_runs_the_case() { # <makefile>
	local recipe="$temporary/recipe.txt"
	make --no-print-directory -n -C "$temporary" -f "$1" check > "$recipe" 2>/dev/null || true
	grep -qx 'bash scripts/ci_browser_host_share_test.sh' "$recipe"
}
check_recipe_runs_the_case "$root/Makefile" || fail 'make check no longer runs bash scripts/ci_browser_host_share_test.sh'
awk 'index($0, "\tbash scripts/ci_browser_host_share_test.sh") != 1' "$root/Makefile" > "$temporary/Makefile-lost-line"
if check_recipe_runs_the_case "$temporary/Makefile-lost-line"; then
	fail 'the recipe case accepted a check goal that dropped the guard'
fi
echo "ok   make check runs the guard's case, and a recipe that lost it is refused"
