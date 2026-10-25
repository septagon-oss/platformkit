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
mutate '.fixtures[0].sha256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"' rewritten-fixture.json
mutate '.fixtures[0].path = "scripts/testdata/rehearse/current-schema.sql"' absent-fixture.json
mutate 'del(.fixtures)' no-fixtures.json
mutate '.required_roles = []' no-roles.json
mutate '.record = "platformkit.supported-sources.v0"' other-kind.json
# The promise may widen, and it may narrow by a written refusal. It may not narrow by
# deleting a case, and it may not state an obligation it never explains.
mutate 'del(.sources[] | select(.ref == "v1.1.1")) | .required_roles = ["oldest_supported"] |
	.refused += [{ref: "v1.1.1", commit: "13d2691a2db9bc38c18828e81994ae57e699c84a",
	              reason: "docs/COMPATIBILITY.md would have to say so, and the owner has not said it"}]' \
	refused-instead.json
mutate 'del(.refused[] | select(.ref == "v1.0.0"))' unasked-tag.json
mutate '.sources[0].why = ""' obligation-without-why.json

run 1 "a proof build listed as supported" "$reader" --record "$work/proof-as-supported.json" verify
run 1 "a ref that names no revision" "$reader" --record "$work/missing-rev.json" verify
run 1 "a ref that is not the commit beside it" "$reader" --record "$work/drifted.json" verify
run 1 "an obligation no source discharges" "$reader" --record "$work/undischarged.json" verify
run 1 "a role the release never required" "$reader" --record "$work/invented-role.json" verify
run 1 "a refused source with no reason" "$reader" --record "$work/reasonless.json" verify
run 1 "an applied migration this release rewrote" "$reader" --record "$work/rewritten-bytes.json" verify
run 1 "an applied migration this tree does not hold" "$reader" --record "$work/absent-bytes.json" verify
run 1 "a record that owes no applied bytes" "$reader" --record "$work/no-bytes.json" verify
run 1 "a rehearsal fixture this release rewrote" "$reader" --record "$work/rewritten-fixture.json" verify
run 1 "a rehearsal fixture this tree does not hold" "$reader" --record "$work/absent-fixture.json" verify
run 1 "a record that owes no fixture" "$reader" --record "$work/no-fixtures.json" verify
run 1 "a record that requires nothing" "$reader" --record "$work/no-roles.json" verify
run 1 "a record of a kind this reader does not read" "$reader" --record "$work/other-kind.json" verify
run 0 "a promised revision that leaves the set by being refused with a reason" "$reader" --record "$work/refused-instead.json" verify
run 1 "a published tag this record answers nothing for" "$reader" --record "$work/unasked-tag.json" verify
run 1 "an obligation with no reason stated beside it" "$reader" --record "$work/obligation-without-why.json" verify
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
# The evidence a case keeps beside its receipt, as the real run writes it: the applied
# ledger the installation arrived with, the ledger it left with (those same rows plus what
# it applied), and the facts before and after. scripts/check_pin_rehearsal.sh re-takes every
# figure below from these bytes, so the fixture has to carry them the way a run does —
# 24 arrived rows, 90 after, all 24 still there, 12 fact lines, 6 boundary reads of a
# neighbour that stays invisible, and 90 queued outbox rows.
ledger_before="$work/before-ledger.tsv"
ledger_after="$work/after-ledger.tsv"
facts_before="$work/before-facts.tsv"
facts_after="$work/after-facts.tsv"
awk 'BEGIN { for (i = 1; i <= 24; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-01 00:00:00+00\n", i, i, i }' \
	</dev/null >"$ledger_before"
cp "$ledger_before" "$ledger_after"
awk 'BEGIN { for (i = 25; i <= 90; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-02 00:00:00+00\n", i, i, i }' \
	</dev/null >>"$ledger_after"
awk 'BEGIN { for (i = 1; i <= 12; i++) printf "contents/rehearse\t%d\t0\n", i * 5 }' </dev/null >"$facts_before"
{
	awk 'BEGIN { for (i = 1; i <= 6; i++) printf "other/rehearse%02d/users\t10\t0\n", i }' </dev/null
	printf 'outbox.pending\t90\t0\n'
} >"$facts_after"
matrix() {
	jq -n --arg t "$target" --arg digest "$digest" \
		 --arg ev_bl "$ledger_before" --arg ev_al "$ledger_after" \
		 --arg ev_bf "$facts_before" --arg ev_af "$facts_after" \
		 --argjson fixtures "$("$reader" fixtures | jq -R -s 'split("\n") | map(select(length > 0) | split("\t") | {path: .[0], sha256: .[1]})')" \
		 --slurpfile r <("$reader" refused | cut -f1,2,3 |
		jq -R -r 'split("\t") | select(length > 0) | {ref: .[0], commit: .[1]}') --slurpfile s <("$reader" positives |
		jq -R -r 'split("\t") | {ref: .[0], commit: .[1], roles: (.[2] | split(","))}') '
		{record: "platformkit.rehearsal.v2", target_commit: $t, target_line: "v2.0.0",
		 support_record: {file: "scripts/testdata/rehearse/supported-sources.json", sha256: $digest},
		 compatibility: {ran: true, exit: 0, command: "python3 scripts/check_public_api.py --line-record scripts/baselines/public-api-v2.0.0-candidate.json HEAD", frozen_commit: "45fbddf52d8432b576deaeba4e74d99c41ed3c97", verdict: "no exported incompatibility"},
		 fixtures: $fixtures,
		 sources: [$s[] | {ref, commit, roles, rehearsal: {
		   base_commit: .commit, candidate_commit: $t, exit: 0,
		   fixtures: $fixtures,
		   data: {facts_lines: 12, boundary_reads: 6, foreign_rows_visible: 0,
		          applied_rows_before: 24, applied_rows_after: 90, applied_rows_retained: 24,
		          pending_outbox_rows: 90},
		   evidence: {before_ledger: $ev_bl, after_ledger: $ev_al,
		              before_facts: $ev_bf, after_facts: $ev_af},
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
jq '.sources[0].rehearsal.fixtures = [{path: "scripts/testdata/rehearse/seed.sql", sha256: "0000000000000000000000000000000000000000000000000000000000000000"}]' \
	"$work/good.json" >"$work/swapped-fixture.json"
jq 'del(.sources[1].rehearsal.fixtures)' "$work/good.json" >"$work/nothing-seeded.json"
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
checkcase 1 "a case that seeded a fixture the record does not owe" "$work/swapped-fixture.json"
checkcase 1 "a case that named nothing it seeded" "$work/nothing-seeded.json"
jq '.compatibility.ran = false' "$work/good.json" >"$work/no-api-check.json"
jq '.compatibility.exit = 1' "$work/good.json" >"$work/api-moved.json"
checkcase 1 "a matrix that recorded no exported-surface comparison" "$work/no-api-check.json"
checkcase 1 "a matrix whose exported surface moved" "$work/api-moved.json"
# The data half is read here as an obligation and not as numbers beside one: a receipt whose
# figures say no boundary was read, a neighbour was visible, or applied rows went missing
# refuses the release with every case inside it green.
jq '.sources[0].rehearsal.data.boundary_reads = 0' "$work/good.json" >"$work/no-boundary-read.json"
jq '.sources[0].rehearsal.data.foreign_rows_visible = 40' "$work/good.json" >"$work/neighbour-visible.json"
jq '.sources[0].rehearsal.data.applied_rows_before = 0' "$work/good.json" >"$work/no-starting-ledger.json"
jq '.sources[1].rehearsal.data.applied_rows_retained = 12' "$work/good.json" >"$work/rows-not-retained.json"
jq 'del(.refused)' "$work/good.json" >"$work/no-refused-key.json"
# The figures are claims; the four files beside the receipt are the evidence. A case that
# kept none can be checked by nobody but the run that wrote it, and a case whose numbers do
# not match those bytes is a receipt from a different run than its own evidence — including
# the one where every recorded figure still reads "all retained" while the ledger the case
# kept lost one of the rows it arrived with.
jq 'del(.sources[0].rehearsal.evidence)' "$work/good.json" >"$work/no-evidence.json"
jq '.sources[0].rehearsal.data.facts_lines = 13' "$work/good.json" >"$work/figures-moved.json"
awk 'BEGIN { for (i = 1; i <= 23; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-01 00:00:00+00\n", i, i, i;
	for (i = 25; i <= 91; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-02 00:00:00+00\n", i, i, i }' \
	</dev/null >"$work/lost-row-after-ledger.tsv"
[ "$(grep -c '' <"$work/lost-row-after-ledger.tsv")" = 90 ] || {
	echo "FAIL  the ledger that lost one arrived row is $(grep -c '' <"$work/lost-row-after-ledger.tsv") line(s), not the 90 the receipt records: the case below would refuse for the wrong reason"
	fails=$((fails + 1))
}
jq --arg p "$work/lost-row-after-ledger.tsv" \
	'.sources[0].rehearsal.evidence.after_ledger = $p' "$work/good.json" >"$work/lost-one-row.json"
checkcase 1 "a case that read no tenant boundary" "$work/no-boundary-read.json"
checkcase 1 "a case whose application role saw another tenant's rows" "$work/neighbour-visible.json"
checkcase 1 "a case that arrived with no applied migration" "$work/no-starting-ledger.json"
checkcase 1 "a case that did not retain the applied rows it arrived with" "$work/rows-not-retained.json"
checkcase 1 "a case that kept no evidence of what it measured" "$work/no-evidence.json"
checkcase 1 "a case whose figures do not match the evidence it kept" "$work/figures-moved.json"
checkcase 1 "a case that claims every applied row and kept a ledger that lost one" "$work/lost-one-row.json"
checkcase 1 "a matrix with no refused list at all" "$work/no-refused-key.json"
checkcase 1 "a single-revision receipt in release mode" "$work/legacy.json"

if [ "$fails" -gt 0 ]; then echo "supported-sources: $fails case(s) failed"; exit 1; fi
echo "supported-sources: every support verdict the record and the checker can give is a case"
