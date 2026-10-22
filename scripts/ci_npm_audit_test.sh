#!/usr/bin/env bash
# A registry that will not answer the audit is tried again; an audit that answered is refused at once.
#
# Forge run 8105, job 77227 lost the design job's `Native design conformance and dependency audit`
# with `npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk`
# and `npm error audit endpoint returned an error`, after the install in the same step had spent five
# minutes on the same host. Nothing in the tree was measured, and `main` passed the same step over the
# same lock bytes twenty minutes earlier (run 75584, job 77138).
#
# Both ways of getting a retry wrong are refused here, in the shape scripts/ci_apt_index_test.sh
# refuses its two: giving up on the first blip is what was red, and retrying forever is a hang a reader
# sees as a hung job rather than a hung registry. The third way is the one a retry loop invites and the
# one this file exists to close — an audit that reported a vulnerable version and exited 1 being retried
# into a paragraph. The case below plants that report and requires the first try to be the last.
#
# The classifier is asked directly by sourcing scripts/ci_npm_audit.sh, so a failure text is answered in
# microseconds; the loop runs against a stub command that records every call it receives; and the
# workflow's own audit line is executed the same way, with `npm` itself stubbed, so a gate that dropped
# the retry, or wrapped the audit in something that swallows what it prints, dies here rather than in a
# job page three hours later.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/ci-npm-audit-XXXXXX")"
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

# The refusal job 77227 died on, word for word from its log at 02:47:02Z.
TIMEOUT='npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk
undefined
npm error audit endpoint returned an error'

# The refusal the step exists to produce, in the shape forge job 51838 wrote on 2026-10-06.
ADVISORY='Package  Version  Dependency  Dev  Severity
source-map-js  1.2.1  css-tree  no  high
Severity: high  GHSA-68fv-2mgg-jv7q
2 high severity vulnerabilities'

classify() { # classify <what npm wrote> — found, transient or final
	(
		# shellcheck source=scripts/ci_npm_audit.sh
		source "$root/scripts/ci_npm_audit.sh"
		npm_audit_classify "$1"
	)
}

check "a feed that did not answer is a route failure" transient "$(classify "$TIMEOUT")"
check "an audit that named a finding is a verdict" found "$(classify "$ADVISORY")"
check "an audit that named a finding and then lost the feed is still a verdict" found \
	"$(classify "$ADVISORY
$TIMEOUT")"
check "an unknown npm failure is final" final "$(classify 'npm error code ERESOLVE')"
check "an install that worked is final" final "$(classify 'added 119 packages in 5m')"

# The loop, against a stub that counts its own calls. What it answers comes from MODE, so one stub
# stands in for the blip, the dead registry, the finding and the answer that was fine.
cat >"$scratch/stub.sh" <<'STUB'
#!/usr/bin/env bash
n="$(cat "$COUNT" 2>/dev/null || echo 0)"; n=$((n + 1)); echo "$n" >"$COUNT"; echo "$*" >>"$CALLS"
case "$MODE" in
	blip)
		printf '%s\n' 'npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk' 'npm error audit endpoint returned an error' >&2
		if [ "$n" -lt "${CLEARS_AT:-99}" ]; then exit 1; fi
		exit 0
		;;
	dead)
		printf '%s\n' 'npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk' 'npm error audit endpoint returned an error' >&2
		exit 1
		;;
	finding)
		printf '%s\n' 'source-map-js  1.2.1  css-tree  no  high' 'Severity: high  GHSA-68fv-2mgg-jv7q' >&2
		exit 1
		;;
	quiet) exit 0 ;;
esac
STUB
chmod +x "$scratch/stub.sh"

run_stub() { # run_stub <mode> — prints the exit status; the call count lands in $scratch/<mode>.count
	local mode="$1" status=0
	(
		MODE="$mode" COUNT="$scratch/$mode.count" CALLS="$scratch/$mode.calls" \
			CLEARS_AT="${CLEARS_AT:-}" PKIT_NPM_AUDIT_WAITS="${PKIT_NPM_AUDIT_WAITS:-0 0}" \
			bash "$root/scripts/ci_npm_audit.sh" "$scratch/stub.sh" >/dev/null 2>&1
	) || status=$?
	printf '%s' "$status"
}

calls() { # calls <mode> — how many times the loop asked the stub
	[ -f "$scratch/$1.count" ] && cat "$scratch/$1.count" || printf 0
}

CLEARS_AT=3
check "a route failure that clears is answered" 0 "$(run_stub blip)"
check "a route failure that clears is retried rather than refused at once" 3 "$(calls blip)"

PKIT_NPM_AUDIT_WAITS="0 0"
check "a registry that stays down ends the step non-zero" 1 "$(run_stub dead)"
check "a registry that stays down is tried a bounded number of times" 3 "$(calls dead)"
check "an audit that named a finding is refused" 1 "$(run_stub finding)"
check "an audit that named a finding is refused on its first try" 1 "$(calls finding)"
check "an audit that found nothing is answered" 0 "$(run_stub quiet)"
check "an audit that found nothing is asked once" 1 "$(calls quiet)"

# What the workflows file actually asks. The step body is read out of ci.yml rather than copied here,
# so a gate that dropped the retry, or dropped the audit itself, fails this case and not a comment.
python3 - >"$scratch/step.sh" <<'PY'
import sys
import yaml

with open(".gitea/workflows/ci.yml") as f:
    jobs = (yaml.safe_load(f) or {}).get("jobs") or {}
for step in (jobs.get("design") or {}).get("steps") or []:
    if "npm audit" in str(step.get("run") or ""):
        print(step["run"])
        break
else:
    sys.exit("FAIL: the design job runs no npm audit step")
PY
grep -Eq '^\s*bash [^ ]*scripts/ci_npm_audit\.sh npm audit --omit=dev --audit-level=high\s*$' "$scratch/step.sh" \
	&& check "the design job's audit gate goes through the retry" ok ok \
	|| check "the design job's audit gate goes through the retry" ok missing
grep -Eq '^\s*npm ci --ignore-scripts\s*$' "$scratch/step.sh" \
	&& check "the design job still installs the lock before it audits" ok ok \
	|| check "the design job still installs the lock before it audits" ok missing
grep -Eq '^\s*npm test\s*$' "$scratch/step.sh" \
	&& check "the design job still runs its tests after the audit" ok ok \
	|| check "the design job still runs its tests after the audit" ok missing

# The gate's own audit line, under the same `bash -e` and the same relative path the job uses, with
# `npm` stubbed to the timeout that was red: three tries of the audit it names, then the step ends.
audit_line="$(grep -Eo 'bash [^ ]*scripts/ci_npm_audit\.sh npm audit .*' "$scratch/step.sh" | head -1)"
mkdir -p "$scratch/bin"
printf '%s\n' '#!/usr/bin/env bash' 'echo "$*" >> "$STUB_ARGS"' \
	'echo "npm warn audit network timeout at: https://registry.npmjs.org/-/npm/v1/security/advisories/bulk" >&2' \
	'echo "npm error audit endpoint returned an error" >&2' 'exit 1' >"$scratch/bin/npm"
chmod +x "$scratch/bin/npm"
mkdir -p "$scratch/job/tools/designexport/openpencil" "$scratch/job/scripts"
ln -s "$root/scripts/ci_npm_audit.sh" "$scratch/job/scripts/ci_npm_audit.sh"
status=0
(
	cd "$scratch/job/tools/designexport/openpencil" || exit 1
	PATH="$scratch/bin:$PATH" STUB_ARGS="$scratch/args" PKIT_NPM_AUDIT_WAITS="0 0" \
		bash -e -c "$audit_line" >/dev/null 2>&1
) || status=$?
check "the gate's own audit line ends non-zero when the feed stays down" 1 "$status"
check "the gate's own audit line tried the feed a bounded number of times" 3 "$(wc -l <"$scratch/args" | tr -d ' ')"
check "the gate's own audit line asked the audit it names" \
	"audit --omit=dev --audit-level=high" "$(tail -1 "$scratch/args")"

# One case pays the real waits, so the seconds at the top of the file stay pinned by behaviour.
real_status=0
(
	MODE=dead COUNT="$scratch/real.count" CALLS="$scratch/real.calls" PKIT_NPM_AUDIT_WAITS="" \
		timeout 120 bash "$root/scripts/ci_npm_audit.sh" "$scratch/stub.sh" >/dev/null 2>&1
) || real_status=$?
check "the real waits outlast a blip and then refuse" 1 "$real_status"
check "the real waits still bound the tries at three" 3 "$(cat "$scratch/real.count")"

if [ "$failures" -ne 0 ]; then
	echo "ci_npm_audit: $failures failure(s)"
	exit 1
fi
echo "ci_npm_audit: the feed is retried, the finding is not"
