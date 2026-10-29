#!/usr/bin/env bash
# A matrix case is refused when the bytes it kept contradict the figures it recorded.
#
# scripts/check_pin_rehearsal.sh --strict re-takes the seven `data` figures of a case from
# the four files that case kept beside its receipt — the applied ledger before and after,
# the facts before and after. The cases where the receipt is the pessimist (it recorded no
# tenant-boundary read, or rows of a neighbour) already exist. The cases where the receipt
# is the optimist are the ones a release note would quote without checking: a run that saw
# another tenant's rows and recorded zero, a run that read no boundary and recorded twelve,
# a run that lost an applied row and recorded them all retained.
#
# Each refusal is paired with the same receipt against evidence that agrees with it, so a
# green line here means the refusal named the figure and not an unrelated complaint about
# the fixture. No database and no network: the target revision is named by
# REHEARSE_PIN_REVISION, the same override a consumer with a private mirror uses.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
reader="$root/scripts/supported_sources.sh"
check="$root/scripts/check_pin_rehearsal.sh"
shipped="$root/scripts/testdata/rehearse/supported-sources.json"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0

mkdir -p "$work/consumer"
printf 'module example.com/consumer\n\ngo 1.25\n\nrequire github.com/septagon-oss/platformkit v2.0.0\n' >"$work/consumer/go.mod"
target=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
digest=$(sha256sum "$shipped" | cut -d' ' -f1)

# The four files one case keeps, as the run writes them: 24 applied rows it arrived with,
# 90 it left with (those same rows plus what it applied), 12 fact lines, 12 boundary reads
# each seeing one tenant and none of a neighbour, and 90 queued outbox rows.
before_ledger="$work/before-ledger.tsv"
after_ledger="$work/after-ledger.tsv"
before_facts="$work/before-facts.tsv"
after_facts="$work/after-facts.tsv"
boundary_facts="$work/after-facts-boundary.tsv"
awk 'BEGIN { for (i = 1; i <= 24; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-01 00:00:00+00\n", i, i, i }' \
	</dev/null >"$before_ledger"
cp "$before_ledger" "$after_ledger"
awk 'BEGIN { for (i = 25; i <= 90; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-02 00:00:00+00\n", i, i, i }' \
	</dev/null >>"$after_ledger"
awk 'BEGIN { for (i = 1; i <= 12; i++) printf "contents/rehearse\t%d\t0\n", i * 5 }' </dev/null >"$before_facts"
# The boundary reads and the queue, written once and reused: each mutant below changes one
# number in this file and nothing else, so the figure that refuses is the one it moved.
{
	awk 'BEGIN { for (i = 1; i <= 12; i++) printf "other/rehearse%02d/users\t10\t0\n", i }' </dev/null
	printf 'outbox.pending\t90\t0\n'
} >"$boundary_facts"
cp "$boundary_facts" "$after_facts"
awk -F'\t' 'BEGIN { OFS = "\t" } $1 ~ /^other\// { $3 = 60 } { print }' "$boundary_facts" >"$work/leak-visible.tsv"
awk -F'\t' 'BEGIN { OFS = "\t" } $1 ~ /^other\// && ++seen <= 2 { print } $1 == "outbox.pending" { print }' \
	"$boundary_facts" >"$work/two-reads.tsv"
# The ledger the case kept holds one arrived row fewer than the case recorded: the after
# file is still 90 lines, because the release did apply 66 files, and the row that went
# missing is the one the immutability rule exists for.
awk 'BEGIN { for (i = 2; i <= 24; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-01 00:00:00+00\n", i, i, i;
	for (i = 25; i <= 90; i++) printf "audit/%02d\t0000%02d_x.up.sql\tchk%02d\t2026-10-02 00:00:00+00\n", i, i, i }' \
	</dev/null >"$work/lost-arrived-row.tsv"

receipt() { # $1 = where to write, $2 = after-facts file, $3 = after-ledger file
	jq -n --arg t "$target" --arg digest "$digest" \
		--arg ev_bl "$before_ledger" --arg ev_al "$3" \
		--arg ev_bf "$before_facts" --arg ev_af "$2" \
		--argjson fixtures "$("$reader" fixtures | jq -R -s 'split("\n") | map(select(length > 0) | split("\t") | {path: .[0], sha256: .[1]})')" \
		--slurpfile r <("$reader" refused | cut -f1,2,3 |
			jq -R -r 'split("\t") | select(length > 0) | {ref: .[0], commit: .[1]}') \
		--slurpfile s <("$reader" positives |
			jq -R -r 'split("\t") | {ref: .[0], commit: .[1], roles: (.[2] | split(","))}') \
		'{record: "platformkit.rehearsal.v2", target_commit: $t, target_line: "v2.0.0",
		  support_record: {file: "scripts/testdata/rehearse/supported-sources.json", sha256: $digest},
		  compatibility: {ran: true, exit: 0, command: "check_public_api.py", verdict: "no exported incompatibility"},
		  fixtures: $fixtures,
		  sources: [$s[] | {ref, commit, roles, rehearsal: {
		    base_commit: .commit, candidate_commit: $t, exit: 0,
		    fixtures: $fixtures,
		    data: {facts_lines: 12, boundary_reads: 12, foreign_rows_visible: 0,
		           applied_rows_before: 24, applied_rows_after: 90, applied_rows_retained: 24,
		           pending_outbox_rows: 90},
		    evidence: {before_ledger: $ev_bl, after_ledger: $ev_al,
		               before_facts: $ev_bf, after_facts: $ev_af},
		    files: [{owner: "audit", version: "48", name: "000048_audit_history_append_only.up.sql",
		             phase: "expand", seconds: 0.002}]}}],
		  refused: [$r[] | {ref, commit: (if .commit == "" then null else .commit end),
		                    reason: "refused by the record", refused_with: 1}],
		  tool: "scripts/rehearse_migrations.sh", mode: "supported", exit: 0}' >"$1"
}

certify() { # $1 = receipt file, $2 = expected exit, $3 = label
	local want="$2" label="$3" out status
	out=$(cd "$work/consumer" && REHEARSE_PIN_REVISION="$target" \
		"$check" --strict --receipt "$1" 2>&1)
	status=$?
	if [ "$status" != "$want" ]; then
		echo "FAIL  $label: exit $status, want $want"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
		return
	fi
	echo "ok    $label"
}

receipt "$work/good.json" "$after_facts" "$after_ledger"
certify "$work/good.json" 0 "the receipt, against evidence that agrees with every figure"

receipt "$work/leak.json" "$work/leak-visible.tsv" "$after_ledger"
certify "$work/leak.json" 1 "a case that recorded no neighbour row over evidence that saw sixty"

receipt "$work/no-reads.json" "$work/two-reads.tsv" "$after_ledger"
certify "$work/no-reads.json" 1 "a case that recorded twelve boundary reads over the two it took"

receipt "$work/lost-row.json" "$after_facts" "$work/lost-arrived-row.tsv"
certify "$work/lost-row.json" 1 "a case that recorded every arrived row as retained over a ledger that lost one"

if [ "$fails" -gt 0 ]; then echo "evidence-vs-receipt: $fails case(s) failed"; exit 1; fi
echo "evidence-vs-receipt: every figure a case records is re-taken from the bytes it kept, in both directions"
