#!/usr/bin/env bash
# A support record that promises less than the record this repository holds is a narrowed
# promise, and the reader of the record must say so rather than certify it.
#
# scripts/testdata/rehearse/supported-sources.json decides which installations a release
# undertakes to upgrade, and scripts/check_pin_rehearsal.sh --strict takes its cases from
# that one file. So the file is also the one edit that makes a release cover less: drop a
# supported source together with the obligation it discharged and the record stays
# internally consistent — every remaining role is discharged once, every ref resolves,
# every owed digest still hashes — the matrix runs one case instead of two, and the
# checker reads the narrowed record as the whole of the promise. That is a passing
# rehearsal for a promise nobody made, and it is the failure a coverage check exists to
# catch: a required source is removed by an owner's decision recorded somewhere a person
# can read, never by the same diff that stops rehearsing it.
#
# The cases are therefore: a record that reaches back less far than the one this tree
# holds, and a record that reaches back only as far as the oldest of them, each refused;
# and the two shapes a release is allowed to ship — the record as it stands, and that
# record with one source added and its own obligation required — each accepted, so the
# guard cannot harden into a refusal of any change at all.
#
# No database, no network, no git object beyond what the reader already resolves.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
reader="$root/scripts/supported_sources.sh"
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

# The release as this tree ships it: every obligation discharged, and the reader answers.
run 0 "the shipped record answers" "$reader" verify

narrow() { jq "$1" "$shipped" >"$work/$2"; }
# The later installed patch and the obligation it discharged, gone together: what is left
# still holds together on its own terms, which is exactly why a self-consistency check
# cannot see this one.
narrow 'del(.sources[] | select(.ref == "v1.1.1")) | .required_roles = ["oldest_supported"]' \
	narrowed-to-oldest.json
# The same halving from the other end: the oldest installation drops out and the promise
# keeps only the patch that sits on top of it.
narrow 'del(.sources[] | select(.ref == "v1.1.0")) | .required_roles = ["later_installed_supported"]' \
	narrowed-to-patch.json
# A release that reaches further than this one does: a revision the record refuses becomes
# a supported source under an obligation of its own. This is allowed, and a guard that
# refuses it refuses the only direction a support record should move.
narrow '.required_roles += ["shipped_bytes"] | .sources += [{"ref": "5ac6c060783ebc9587d4b9c23c030f371b4d7e9d", "commit": "5ac6c060783ebc9587d4b9c23c030f371b4d7e9d", "roles": ["shipped_bytes"], "why": "a record may take on more than it promised before"}]' \
	widened.json

run 1 "a record that drops its later supported installation" \
	"$reader" --record "$work/narrowed-to-oldest.json" verify
run 1 "a record that drops its oldest installation" \
	"$reader" --record "$work/narrowed-to-patch.json" verify
run 0 "a record that promises one installation more" \
	"$reader" --record "$work/widened.json" verify

if [ "$fails" -gt 0 ]; then
	echo "a-narrowed-support-record: $fails case(s) failed" >&2
	exit 1
fi
echo "a-narrowed-support-record: ok — a record that promises less than this tree already promises is refused, and one that promises more is not"
