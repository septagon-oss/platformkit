#!/usr/bin/env bash
# What the supported-source record and the two scripts that read it refuse, on fixtures.
#
# The record decides which installations a release promises to upgrade, so the cases here
# are the ones that matter: a source is supported by its revision resolving to what the
# record says, an obligation is discharged by exactly one source, a proof build is not
# made supported by being listed, and a refusal is a named refusal with a reason rather
# than an omission. The checker half asks the same questions of a matrix receipt: a case
# missing from it, a case repeated in place of another, an identity that drifted, a record
# digest that is not this release's, and a single-revision receipt handed to release mode.
#
# No database and no network: the pin's commit is named by REHEARSE_PIN_REVISION, which is
# the same override a consumer with a private mirror uses.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
reader="$root/scripts/supported_sources.sh"
check="$root/scripts/check_pin_rehearsal.sh"
shipped="$root/scripts/testdata/rehearse/supported-sources.json"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0

run() { # $1 = expected exit, $2 = label, then the command
	local want="$1" label="$2"
	shift 2
	local out status
	out=$("$@" 2>&1)
	status=$?
	if [ "$status" != "$want" ]; then
		echo "FAIL  $label: exit $status, want $want"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
	else
		echo "ok    $label"
	fi
}

# --- the record -------------------------------------------------------------------
run 0 "the shipped record answers" "$reader" verify
rows=$("$reader" positives | grep -c '')
roles=$(jq -r '.required_roles[]' "$shipped" | sort | paste -sd,)
discharged=$("$reader" positives | cut -f3 | tr ',' '\n' | sort -u | paste -sd,)
if [ "$rows" != "$(jq -r '.required_roles | length' "$shipped")" ] || [ "$roles" != "$discharged" ]; then
	echo "FAIL  every required role is discharged by exactly one supported source: $rows row(s), roles [$roles] against [$discharged]"
	fails=$((fails + 1))
else
	echo "ok    every required role is discharged by exactly one supported source"
fi

mutate() { jq "$1" "$shipped" >"$work/$2"; }
mutate '.sources[0].ref = "v1.1.2-proof.20261018.b8f82e073f67"' proof-as-supported.json
mutate '.sources[0].ref = "no-such-branch-of-this-repository"' missing-rev.json
mutate '.sources[0].commit = "0000000000000000000000000000000000000000"' drifted.json
mutate '.sources[0].roles = []' undischarged.json
mutate '.sources[0].roles += ["every_release_ever"]' invented-role.json
mutate '.refused[0].reason = ""' reasonless.json
mutate '.applied_bytes[0].sha256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"' rewritten-bytes.json
mutate '.applied_bytes[0].path = "modules/audit/migrations/000099_that_was_never_applied.up.sql"' absent-bytes.json
mutate 'del(.applied_bytes)' no-bytes.json
mutate '.required_roles = []' no-roles.json
mutate '.record = "platformkit.supported-sources.v0"' other-kind.json

run 1 "a proof build listed as supported" "$reader" --record "$work/proof-as-supported.json" verify
run 1 "a ref that names no revision" "$reader" --record "$work/missing-rev.json" verify
run 1 "a ref that is not the commit beside it" "$reader" --record "$work/drifted.json" verify
run 1 "an obligation no source discharges" "$reader" --record "$work/undischarged.json" verify
run 1 "a role the release never required" "$reader" --record "$work/invented-role.json" verify
run 1 "a refused source with no reason" "$reader" --record "$work/reasonless.json" verify
run 1 "an applied migration this release rewrote" "$reader" --record "$work/rewritten-bytes.json" verify
run 1 "an applied migration this tree does not hold" "$reader" --record "$work/absent-bytes.json" verify
run 1 "a record that owes no applied bytes" "$reader" --record "$work/no-bytes.json" verify
run 1 "a record that requires nothing" "$reader" --record "$work/no-roles.json" verify
run 1 "a record of a kind this reader does not read" "$reader" --record "$work/other-kind.json" verify
run 2 "no record at all" "$reader" --record "$work/not-here.json" verify

supported_ref=$(jq -r '.sources[0].ref' "$shipped")
refused_ref=$(jq -r '.refused[0].ref' "$shipped")
run 0 "a supported source gets its verdict" "$reader" verdict "$supported_ref"
run 1 "a refused source gets its reason" "$reader" verdict "$refused_ref"
run 1 "a revision nobody named is refused, not skipped" "$reader" verdict HEAD

# --- the matrix receipt -----------------------------------------------------------
mkdir -p "$work/consumer"
printf 'module example.com/consumer\n\ngo 1.25\n\nrequire github.com/septagon-oss/platformkit v2.0.0\n' \
	>"$work/consumer/go.mod"
target=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
digest=$(sha256sum "$shipped" | cut -d' ' -f1)
matrix() {
	jq -n --arg t "$target" --arg digest "$digest" --slurpfile r <("$reader" refused | cut -f1,2,3 |
		jq -R -r 'split("\t") | select(length > 0) | {ref: .[0], commit: .[1]}') --slurpfile s <("$reader" positives |
		jq -R -r 'split("\t") | {ref: .[0], commit: .[1], roles: (.[2] | split(","))}') '
		{record: "platformkit.rehearsal.v2", target_commit: $t, target_line: "v2.0.0",
		 support_record: {file: "scripts/testdata/rehearse/supported-sources.json", sha256: $digest},
		 sources: [$s[] | {ref, commit, roles, rehearsal: {
		   base_commit: .commit, candidate_commit: $t, exit: 0,
		   files: [{owner: "audit", version: "48", name: "000048_audit_history_append_only.up.sql",
		            phase: "expand", seconds: 0.002}]}}],
		 refused: [$r[] | {ref, commit: (if . == "" then null else . end), reason: "refused by the record", refused_with: 1}],
		 tool: "scripts/rehearse_migrations.sh", mode: "supported", exit: 0}' >"$work/$1"
}
matrix good.json

checkcase() { # $1 = expected exit, $2 = label, $3 = receipt file, remaining = extra args
	local want="$1" label="$2" file="$3"
	shift 3
	local out status
	out=$(cd "$work/consumer" && REHEARSE_PIN_REVISION="$target" \
		"$check" --strict --receipt "$file" "$@" 2>&1)
	status=$?
	if [ "$status" != "$want" ]; then
		echo "FAIL  $label: exit $status, want $want"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
	else
		echo "ok    $label"
	fi
}

checkcase 0 "a matrix with every case the record requires" "$work/good.json"
jq 'del(.sources[0])' "$work/good.json" >"$work/no-oldest.json"
jq '.sources += [.sources[0]]' "$work/good.json" >"$work/duplicated.json"
jq '.sources[1].commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"' "$work/good.json" >"$work/wrong-commit.json"
jq '.target_commit = "cccccccccccccccccccccccccccccccccccccccc"' "$work/good.json" >"$work/wrong-target.json"
jq '.support_record.sha256 = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"' "$work/good.json" >"$work/wrong-record.json"
jq 'del(.refused[0])' "$work/good.json" >"$work/quiet-omission.json"
jq '.sources[2].rehearsal.files = []' "$work/good.json" >"$work/empty-case.json"
jq -n '{base_ref: "v1.1.0", candidate_commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        files: [{owner: "audit", version: "48", name: "x", phase: "expand", seconds: 0.1}],
        max_lock_ms: 0, exit: 0}' >"$work/legacy.json"
checkcase 1 "the oldest supported installation removed" "$work/no-oldest.json"
checkcase 1 "one case repeated in place of another" "$work/duplicated.json"
checkcase 1 "a case whose revision is not the one the record names" "$work/wrong-commit.json"
checkcase 1 "a matrix taken into another revision" "$work/wrong-target.json"
checkcase 1 "a matrix that names another support record" "$work/wrong-record.json"
checkcase 1 "a refused source left out quietly" "$work/quiet-omission.json"
checkcase 1 "a case that applied nothing" "$work/empty-case.json"
checkcase 1 "a single-revision receipt in release mode" "$work/legacy.json"

if [ "$fails" -gt 0 ]; then echo "supported-sources: $fails case(s) failed"; exit 1; fi
echo "supported-sources: every support verdict the record and the checker can give is a case"
