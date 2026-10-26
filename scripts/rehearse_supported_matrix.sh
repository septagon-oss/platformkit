#!/usr/bin/env bash
# The matrix a release is certified by: this rehearsal run once per supported source.
#
#	rehearse_supported_matrix.sh          (budgets from REHEARSE_MAX_FILE_SECONDS and
#	                                       REHEARSE_MAX_LOCK_MS; REHEARSE_RECEIPT names
#	                                       where the one aggregated receipt is written)
#
# scripts/supported_sources.sh answers which installations this release promises to
# upgrade from, and every case here is scripts/rehearse_migrations.sh --base-ref on one
# of those revisions: the copy, the fixtures, the drain, the lock watcher and both
# budgets are the ones a single case already measures, so this file holds no second
# implementation of them. What it adds is the obligation to have run them for *every*
# source the release promises, and a receipt that names each one with its revision, its
# fixtures and what the run measured.
#
# The list of cases comes from the support record and never from anything this run
# writes, which is what keeps a matrix from deciding for itself which cases it owed. A
# source the record refuses is asked for its refusal here, so a refusal is an event that
# happened on this run rather than a source that was quietly left out, and a receipt that
# cannot show both is refused by scripts/check_pin_rehearsal.sh --strict.
#
# The exit code is the case's own: a matrix is only as good as the source that failed,
# and 2 means this file could not run (no record, no tools, no receipt to write).
set -Eeuo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

die() { local code="$1"; shift; echo "rehearse: $*" >&2; exit "$code"; }
note() { echo "rehearse: $*"; }

max_file="${REHEARSE_MAX_FILE_SECONDS:-30}"
max_lock="${REHEARSE_MAX_LOCK_MS:-5000}"
while [ $# -gt 0 ]; do
	case "$1" in
	--max-file-seconds) [ $# -ge 2 ] || die 2 "--max-file-seconds wants a number"; max_file="$2"; shift 2 ;;
	--max-lock-ms) [ $# -ge 2 ] || die 2 "--max-lock-ms wants a number"; max_lock="$2"; shift 2 ;;
	*) die 2 "unknown argument $1; this step takes only the two budgets" ;;
	esac
done
[[ "$max_file" =~ ^[0-9]+$ ]] || die 2 "the file budget is $max_file, which is not a number of seconds"
[[ "$max_lock" =~ ^[0-9]+$ ]] || die 2 "the lock budget is $max_lock, which is not a number of milliseconds"
absent=()
for tool in psql go jq git sha256sum; do
	command -v "$tool" >/dev/null || absent+=("$tool")
done
[ ${#absent[@]} -eq 0 ] || die 2 "not installed: ${absent[*]} (the matrix fails loudly rather than passing quietly)"
[ -n "${PLATFORMKIT_TEST_ADMIN_URL:-}" ] ||
	die 2 "PLATFORMKIT_TEST_ADMIN_URL is unset: every case creates and drops its own two databases through it"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

reader=("$root/scripts/supported_sources.sh")
"${reader[@]}" verify >/dev/null || die $? "the supported-source record does not answer as it stands, so nothing about it can be certified"
"${reader[@]}" positives >"$work/positives.tsv" || die $? "the supported-source record named no case this run could take"
"${reader[@]}" refused >"$work/refused.tsv" || die $? "the supported-source record's refusals could not be read"
candidate=$(git rev-parse HEAD)
record_file="$root/scripts/testdata/rehearse/supported-sources.json"
record_digest=$(sha256sum "$record_file" | cut -d' ' -f1)
target_line=$(jq -r '.target_line' "$record_file")

# The contract half of the same evidence. A release note that says the data survived and
# says nothing about the exported surface an installation compiles against answers only
# one of the two questions a pinned consumer asks, so the API comparison runs here and its
# verdict is recorded beside the cases: the frozen revision, the command, what it returned.
# An incompatibility refuses the matrix rather than being noted under it.
line_record="${APIDIFF_RECORD:-$root/scripts/baselines/public-api-v2.0.0-candidate.json}"
compat='{"ran": false, "reason": "no readable release-line record, or no python3 to compare the surface with"}'
if [ -r "$line_record" ] && command -v python3 >/dev/null; then
	set +e
	python3 "$root/scripts/check_public_api.py" --line-record "$line_record" HEAD \
		>"$work/compat.out" 2>"$work/compat.err"
	compat_code=$?
	set -e
	compat_words=$(cat "$work/compat.out" "$work/compat.err")
	# The comparison prints a JSON report; its own `result` line is the sentence for the
	# receipt, and where the output is not that report, its last line is.
	compat_verdict=$(jq -r '.result // empty' "$work/compat.out" 2>/dev/null || true)
	[ -n "$compat_verdict" ] ||
		compat_verdict=$(printf '%s\n' "$compat_words" | grep -v '^[[:space:]]*$' | tail -1)
	compat=$(jq -n --argjson code "$compat_code" \
		--arg frozen "$(jq -r '.frozen.commit // ""' "$line_record")" \
		--arg command "python3 scripts/check_public_api.py --line-record ${line_record#"$root"/} HEAD" \
		--arg verdict "${compat_verdict:-no output}" \
		'{ran: true, exit: $code, command: $command,
		  frozen_commit: (if $frozen == "" then null else $frozen end), verdict: $verdict}')
	[ "$compat_code" -eq 0 ] ||
		die 1 "the exported surface moved against the line's frozen revision, so this release has no client compatibility to record: $(printf '%s\n' "$compat_words" | tail -3 | tr '\n' ' ')"
	note "contract: $(jq -r '.verdict' <<<"$compat")"
else
	note "contract: the exported surface was not compared ($line_record); the receipt records that it did not run"
fi
n=0
# The four files each case compares (the ledger and the facts, before and after) outlive the
# run that reads them, because scripts/check_pin_rehearsal.sh --strict re-derives the
# receipt's figures from those bytes rather than trusting the run that wanted to pass. Under
# an aggregate receipt every case gets a directory of its own beside it; with no receipt
# named there is nothing for anyone to inspect afterwards, so the evidence goes with the
# temp directory the run already removes.
evidence_root="${REHEARSE_RECEIPT:-}"
case "$evidence_root" in
	"") evidence_root="$work/evidence" ;;
	/*) evidence_root="${evidence_root}.d" ;;
	*) evidence_root="$PWD/${REHEARSE_RECEIPT}.d" ;;
esac
while IFS=$'	' read -r case_ref case_commit case_roles; do
	[ -n "$case_ref" ] || continue
	n=$((n + 1))
	case_receipt="$work/case-$n.json"
	note "case $n: $case_ref (${case_commit:0:12}), discharging ${case_roles:-no named role}"
	set +e
	REHEARSE_RECEIPT="$case_receipt" REHEARSE_EVIDENCE="$evidence_root/case-$n" \
		"$root/scripts/rehearse_migrations.sh" --base-ref "$case_commit" \
		--max-file-seconds "$max_file" --max-lock-ms "$max_lock"
	case_code=$?
	set -e
	[ "$case_code" -eq 0 ] || die "$case_code" "case $case_ref did not pass; the matrix certifies nothing while one of its sources is unmeasured"
	[ -r "$case_receipt" ] || die 2 "case $case_ref exited 0 and left no receipt at $case_receipt"
	# A case that read no tenant boundary says nothing about boundaries, and the number is
	# zero for two very different reasons: an application role the step could not reach, and
	# an installation with no tenant in it. Either way the release cannot claim the promise
	# this mode exists to make, so the case refuses rather than recording a bare zero.
	jq -e '.data.boundary_reads // 0 | . >= 1' "$case_receipt" >/dev/null ||
		die 1 "case $case_ref recorded no tenant-boundary read; the copy was read by no application role, or holds no tenant to read as, and either way the boundary survives nothing"
	jq -e --arg c "$case_commit" --arg t "$candidate" \
		'.base_commit == $c and .candidate_commit == $t' "$case_receipt" >/dev/null ||
		die 1 "case $case_ref's receipt names a base or a candidate other than ${case_commit:0:12} into ${candidate:0:12}"
	jq -n --arg ref "$case_ref" --arg commit "$case_commit" --arg roles "$case_roles" \
		--slurpfile rehearsal "$case_receipt" \
		'{ref: $ref, commit: $commit, roles: ($roles | split(",")), rehearsal: $rehearsal[0]}' \
		>"$work/result-$n.json"
done <"$work/positives.tsv"
[ "$n" -gt 0 ] || die 2 "the record named no supported source to run; a matrix with no case is not a pass"

m=0
while IFS=$'	' read -r refused_ref refused_commit refused_reason; do
	[ -n "$refused_ref" ] || continue
	m=$((m + 1))
	set +e
	refusal=$("$root/scripts/rehearse_migrations.sh" --check-source "$refused_ref" 2>&1)
	refused_code=$?
	set -e
	if [ "$refused_code" -eq 0 ]; then
		echo "REFUSAL DID NOT REFUSE $refused_ref: the support record answered that it is supported"
		die 1 "a source the release does not support was accepted; the refusal is the promise's other half"
	fi
	note "refused $refused_ref as the record requires: $(printf '%s' "$refusal" | head -1)"
	jq -n --arg ref "$refused_ref" --arg commit "$refused_commit" --arg reason "$refused_reason" \
		--argjson refused_with "$refused_code" \
		'{ref: $ref, commit: (if $commit == "" then null else $commit end), reason: $reason,
		  refused_with: $refused_with}' >"$work/refusal-$m.json"
done <"$work/refused.tsv"

if [ -n "${REHEARSE_RECEIPT:-}" ]; then
	mkdir -p "$(dirname "$REHEARSE_RECEIPT")" || die 2 "cannot write the receipt directory for $REHEARSE_RECEIPT"
	# --slurpfile reads one file; the cases are one file each, so they are slurped
	# with -s into a JSON array and handed over as that array.
	cases_json=$(jq -s '.' "$work"/result-*.json 2>/dev/null || echo '[]')
	refusals_json=$(jq -s '.' "$work"/refusal-*.json 2>/dev/null || echo '[]')
	jq -n --arg target_commit "$candidate" --arg target_line "$target_line" \
		--arg record "$record_file" --arg record_digest "$record_digest" \
		--argjson results "$cases_json" --argjson refusals "$refusals_json" \
		--argjson compat "$compat" \
		'{record: "platformkit.rehearsal.v2", target_commit: $target_commit,
		  target_line: $target_line,
		  support_record: {file: $record, sha256: $record_digest},
		  compatibility: $compat,
		  sources: $results, refused: $refusals,
		  tool: "scripts/rehearse_supported_matrix.sh", mode: "supported", exit: 0}' \
		>"$REHEARSE_RECEIPT.tmp" || die 2 "the matrix receipt could not be written to $REHEARSE_RECEIPT.tmp"
	mv -f "$REHEARSE_RECEIPT.tmp" "$REHEARSE_RECEIPT" ||
		die 2 "the matrix receipt could not be moved into place at $REHEARSE_RECEIPT"
	note "receipt: $REHEARSE_RECEIPT — $n supported source(s) upgraded into ${candidate:0:12}, $m refusal(s) exercised"
else
	note "$n supported source(s) upgraded into ${candidate:0:12}, $m refusal(s) exercised; no REHEARSE_RECEIPT was named"
fi
exit 0
