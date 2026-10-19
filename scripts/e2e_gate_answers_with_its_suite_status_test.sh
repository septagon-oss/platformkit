#!/usr/bin/env bash
# Gate 10's step is red exactly when its suite is. The check job's `make e2e` step runs the suite
# inside scripts/ci_browser_host_share.sh's sampler, and the sampler hands the suite's status back;
# a line that swallowed it — `|| true`, a pipe into a command that succeeds, `continue-on-error` —
# would leave the job green over a failing browser suite, and the cases that order this step read
# only its name.
#
# So the case runs the step's own `run` block the way the runner runs it, from a copy of the tree's
# shape: `make` answers 3 and the host's wait is bounded at zero, so nothing starts and nothing
# waits. The step must answer 3, and 0 when `make` does. Three planted copies, one per way of
# swallowing the status, must be refused by the same reading. Writes only into one temporary
# directory; no node, no browser, no database, no listener.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ci="$root/.gitea/workflows/ci.yml"
guard="$root/scripts/ci_browser_host_share.sh"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
failures=0
fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# The step's shell, working directory and run block, read as data. A step is gate 10 when a line of
# its run block that is not prose runs `make e2e`; continue-on-error is refused outright, because it
# turns any status into a pass after the block has answered.
extract() { # <workflow> <out dir>: writes run.sh and shell, or prints why it cannot
	python3 - "$1" "$2" <<'PY'
import os, re, sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    jobs = (yaml.safe_load(handle) or {}).get("jobs") or {}
steps = (jobs.get("check") or {}).get("steps") or []
def runs_gate(step):
    return any(re.search(r"(^|\s)make e2e(\s|$)", line) and not line.strip().startswith("#")
               for line in str(step.get("run") or "").splitlines())
gates = [s for s in steps if runs_gate(s)]
if len(gates) != 1:
    print(f"the check job has {len(gates)} steps that run make e2e, not one")
    sys.exit(0)
step = gates[0]
if step.get("continue-on-error") not in (None, False):
    print(f"the gate's step sets continue-on-error: {step.get('continue-on-error')!r}")
    sys.exit(0)
out = sys.argv[2]
with open(os.path.join(out, "run.sh"), "w", encoding="utf-8") as handle:
    handle.write(str(step["run"]))
with open(os.path.join(out, "shell"), "w", encoding="utf-8") as handle:
    handle.write(str(step.get("shell") or ""))
with open(os.path.join(out, "workdir"), "w", encoding="utf-8") as handle:
    handle.write(str(step.get("working-directory") or "."))
PY
}

# A tree with the guard where the step looks for it, a load average that always has a free share,
# and a `make` that answers what it is told and records that it was asked for e2e.
stage() { # <dir> <make status>
	local dir="$1" status="$2"
	mkdir -p "$dir/tree/scripts" "$dir/bin"
	printf '0.00 0.00 0.00 1/1 1\n' > "$dir/loadavg"
	cat > "$dir/tree/scripts/ci_browser_host_share.sh" <<EOF
#!/usr/bin/env bash
case "\${1:-}" in
	wait) exec bash "$guard" wait 0 "$dir/loadavg" 1 ;;
	*) exec bash "$guard" "\$@" ;;
esac
EOF
	cat > "$dir/bin/make" <<EOF
#!/bin/sh
echo "\$*" >> "$dir/make-calls"
exit $status
EOF
	chmod +x "$dir/bin/make"
}

# The step's answer when `make` answers <status>, run as the runner runs a `run` block: `bash -e`
# when the step names no shell, `bash --noprofile --norc -eo pipefail` when it names bash.
step_status() { # <workflow> <make status>: prints the step's exit code, or the reason it cannot run
	local workflow="$1" status="$2" dir reason shell workdir code
	dir="$(mktemp -d "$temporary/step.XXXXXX")"
	reason="$(extract "$workflow" "$dir")"
	if [ -n "$reason" ]; then
		echo "$reason"
		return
	fi
	stage "$dir" "$status"
	shell="$(cat "$dir/shell")"
	workdir="$(cat "$dir/workdir")"
	mkdir -p "$dir/tree/$workdir"
	code=0
	case "$shell" in
		"") (cd "$dir/tree/$workdir" && PATH="$dir/bin:$PATH" bash -e "$dir/run.sh") >"$dir/out" 2>&1 || code=$? ;;
		bash) (cd "$dir/tree/$workdir" && PATH="$dir/bin:$PATH" bash --noprofile --norc -eo pipefail "$dir/run.sh") >"$dir/out" 2>&1 || code=$? ;;
		*) echo "the gate's step runs under a shell this case does not read: $shell"; return ;;
	esac
	if ! grep -qx 'e2e' "$dir/make-calls" 2>/dev/null; then
		echo "the gate's step never asked make for e2e (exit $code)"
		return
	fi
	echo "$code"
}

got="$(step_status "$ci" 3)"
[ "$got" = 3 ] || fail "gate 10's step answers '$got' when its suite fails with 3"
got="$(step_status "$ci" 0)"
[ "$got" = 0 ] || fail "gate 10's step answers '$got' when its suite passes"
[ "$failures" = 0 ] && echo "ok   gate 10's step answers with its suite's own status, failing and passing"

# The same reading over three planted copies. Each must not answer 3 when the suite fails with 3;
# a copy this case could not tell from the real one would make the case above say nothing.
plant() { # <name> <python expression over `step`>: a copy of ci.yml with gate 10's step changed
	python3 - "$ci" "$temporary/$1.yml" "$2" <<'PY'
import re, sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    doc = yaml.safe_load(handle)
for step in doc["jobs"]["check"]["steps"]:
    if any(re.search(r"(^|\s)make e2e(\s|$)", line) and not line.strip().startswith("#")
           for line in str(step.get("run") or "").splitlines()):
        exec(sys.argv[3])
with open(sys.argv[2], "w", encoding="utf-8") as handle:
    yaml.safe_dump(doc, handle, sort_keys=False)
PY
	echo "$temporary/$1.yml"
}
swallowed="$(plant or-true 'step["run"] = re.sub(r"make e2e[ \t]*$", "make e2e || true", step["run"].rstrip("\n"))')"
piped="$(plant piped 'step["run"] = re.sub(r"make e2e[ \t]*$", "make e2e | tee /dev/null", step["run"].rstrip("\n"))')"
excused="$(plant excused 'step["continue-on-error"] = True')"
refused=0
for planted in "$swallowed" "$piped" "$excused"; do
	got="$(step_status "$planted" 3)"
	if [ "$got" = 3 ]; then
		fail "a step that swallows its suite's status was read as gate 10 answering 3: ${planted##*/}"
	else
		refused=$((refused + 1))
	fi
done
[ "$refused" = 3 ] && echo "ok   a step that swallows its suite's status is told apart from gate 10 (|| true, a pipe, continue-on-error)"

exit $((failures > 0))
