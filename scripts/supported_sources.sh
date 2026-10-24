#!/usr/bin/env bash
# The one reader of the supported-source record.
#
#	supported_sources.sh [--record FILE] (verify | positives | refused | verdict REF)
#
# scripts/testdata/rehearse/supported-sources.json says which installations this
# release promises to upgrade: the exact revision each name is, which obligation it
# discharges, and which revisions are refused with a reason. scripts/rehearse_migrations.sh
# asks it what to rehearse and scripts/check_pin_rehearsal.sh asks it what a receipt
# had to cover — both ask the same file, so a receipt can never decide for itself
# which sources were required.
#
# What "supported" *means* is docs/COMPATIBILITY.md and RELEASE.md; this script never
# derives membership from a version regex or a tag list, because the policy is not a
# pattern. What it does check is the part a record can get wrong silently:
#
#   the shape            a record this script does not understand is not a record
#   a name that is not a revision  every ref must resolve, here, to the commit beside it
#   a proof build called supported  docs/COMPATIBILITY.md gives it no claim to make
#   an undischarged obligation     every required role is discharged exactly once
#   a refusal with no reason       a source is refused by being named, never by being left out
#
# Exit codes:
#
#	0  the record answers, and the answer is what was asked for
#	1  the refusal: a record that does not hold together, or a source not supported
#	2  the record could not be read at all — no file, no JSON, no git to resolve refs
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
record="$root/scripts/testdata/rehearse/supported-sources.json"
verb=""
ref=""
while [ $# -gt 0 ]; do
	case "$1" in
	--record) [ $# -ge 2 ] || { echo "supported-sources: --record wants a file" >&2; exit 2; }; record="$2"; shift 2 ;;
	verify | positives | refused | verdict) verb="$1"; shift ;;
	-h | --help) sed -n '2,26p' "$0"; exit 0 ;;
	*)
		[ -n "$verb" ] || { echo "supported-sources: unknown argument $1" >&2; exit 2; }
		[ -z "$ref" ] || { echo "supported-sources: unknown argument $1" >&2; exit 2; }
		ref="$1"
		shift
		;;
	esac
done
[ -n "$verb" ] || { echo "supported-sources: say what to answer: verify, positives, refused or verdict REF" >&2; exit 2; }
if [ "$verb" = verdict ] && [ -z "$ref" ]; then
	echo "supported-sources: verdict wants a revision" >&2
	exit 2
fi
if [ ! -r "$record" ]; then
	echo "supported-sources: the record at $record cannot be read, and a release with no readable support record has no supported sources" >&2
	exit 2
fi
jq -e . "$record" >/dev/null 2>&1 || { echo "supported-sources: $record is not JSON" >&2; exit 2; }
command -v git >/dev/null || { echo "supported-sources: no git to resolve the record's revisions with" >&2; exit 2; }

refuse() { echo "supported-sources: $*" >&2; exit 1; }

# The shape first, because every later question reads a field of it.
shape=$(jq -r '
	[ (.record // ""), (.target_line // ""), (.required_roles | if type == "array" then length else -1 end),
	  (.sources | if type == "array" then length else -1 end),
	  (.refused | if type == "array" then length else -1 end) ] | @tsv' "$record")
IFS=$'\t' read -r kind target nroles nsources nrefused <<EOF
$shape
EOF
[ "$kind" = "platformkit.supported-sources.v1" ] ||
	refuse "$record says it is a '${kind:-nothing}' record; this reader understands platformkit.supported-sources.v1"
[ -n "$target" ] || refuse "$record names no release line it is the support record of"
[ "$nroles" -gt 0 ] || refuse "$record lists no required role, so nothing in it could be missing"
[ "$nsources" -gt 0 ] || refuse "$record lists no supported source; a release that promises no upgrade says so in prose, not in an empty list"
[ "$nrefused" -ge 0 ] || refuse "$record's refusals are not a list"

# A proof build is refused on its name before anything resolves it: docs/COMPATIBILITY.md
# gives such a tag no compatibility claim, so listing one as supported would promise what
# the policy declines to — and the promise would then be read back out of the record as
# though the record had derived it.
while IFS= read -r name; do
	printf '%s' "$name" | grep -Eq '(^|[.-])(proof|rc[0-9]*|beta|alpha)([.-]|[0-9]|$)' || continue
	refuse "$name is a proof build, which docs/COMPATIBILITY.md gives no compatibility claim; it cannot be a supported source"
done < <(jq -r '.sources[].ref' "$record")

# Every ref is the commit the record says it is, resolved against this repository. A
# record whose ref drifted from its commit would rehearse one revision and certify
# another, which is the failure a receipt cannot see from the inside.
while IFS=$'\t' read -r name want; do
	have=$(git -C "$root" rev-parse --verify --quiet "${name}^{commit}" || true)
	[ -n "$have" ] || refuse "$name does not name a revision of this repository, so the record cannot say an installation ran it"
	[ "$have" = "$want" ] || refuse "$name is ${have:0:12}, and $record says $want: the record and the repository disagree about what to rehearse"
done < <(jq -r '[(.sources + .refused)[] | [.ref, .commit // ""] | @tsv] | .[]' "$record")

# Each obligation, once. A role nobody discharges is the missing revision this record
# exists to prevent; a role two sources discharge is a receipt that could name either;
# and a role outside required_roles is a field nothing checks.
unknown_role=$(jq -r '[ (.sources[].roles // [])[] ] - .required_roles | unique | join(",")' "$record")
[ -z "$unknown_role" ] ||
	refuse "$record gives a source a role the release does not require: $unknown_role (required: $(jq -r '.required_roles | join(",")' "$record"))"

while IFS= read -r role; do
	hits=$(jq -r --arg role "$role" '[ (.sources[].roles // [])[] | select(. == $role) ] | length' "$record")
	[ "$hits" -eq 1 ] || refuse "role $role is discharged by $hits source(s); every obligation is discharged by exactly one revision"
done < <(jq -r '.required_roles[]' "$record")

while IFS= read -r name; do
	refuse "$name is listed as refused with no reason; a source is refused by being named and explained, never by being left out"
done < <(jq -r '.refused[] | select((.reason // "") == "") | .ref' "$record")

# The bytes an installation already holds applied. The runner compares a migration whole,
# so a release that rewrites an applied file — a comment is enough — is refused by every
# ledger that file landed in. The record names those bytes, and they are checked here, on
# the tree the release is built from: a supported source whose files no longer hash to
# what the field owes is a source whose history this release rejects.
nbytes=$(jq -r 'if (.applied_bytes | type) == "array" then (.applied_bytes | length) else 0 end' "$record")
[ "$nbytes" -gt 0 ] ||
	refuse "$record names no applied migration bytes; a supported installation has migrations applied, and the exact bytes it holds are the first thing an upgrade must keep"
while IFS=$'\t' read -r path want; do
	[ -n "$path" ] || continue
	[ -r "$root/$path" ] ||
		refuse "$record owes the applied bytes at $path, which this tree does not hold"
	have=$(sha256sum "$root/$path" | cut -d' ' -f1)
	[ "$have" = "$want" ] ||
		refuse "$path is $have here, and $record owes $want: the release rewrites a migration that installations already have applied"
done < <(jq -r '.applied_bytes[] | [.path, .sha256 // ""] | @tsv' "$record")

case "$verb" in
verify)
	echo "supported-sources: $record — line $target, $nsources supported source(s), $nrefused refused, roles $(jq -r '.required_roles | join(",")' "$record"), $nbytes applied migration byte obligation(s)"
	;;
positives)
	jq -r '.sources[] | [.ref, .commit, ((.roles // []) | join(","))] | @tsv' "$record"
	;;
refused)
	jq -r '.refused[] | [.ref, .commit // "", .reason] | @tsv' "$record"
	;;
verdict)
	line=$(jq -r --arg ref "$ref" '(.sources[] | select(.ref == $ref) | "supported	" + .commit + "	" + ((.roles // []) | join(","))) // empty' "$record")
	if [ -n "$line" ]; then
		IFS=$'\t' read -r _ commit roles <<<"$line"
		echo "supported-sources: $ref (${commit:0:12}) is a supported source for $target, discharging ${roles:-no named role}"
		exit 0
	fi
	reason=$(jq -r --arg ref "$ref" '(.refused[] | select(.ref == $ref) | .reason) // empty' "$record")
	if [ -n "$reason" ]; then
		echo "supported-sources: $ref is refused: $reason" >&2
		exit 1
	fi
	echo "supported-sources: $ref is no supported source of $target and is named in no refusal; an unsupported revision gets a refusal with a reason, not a skip" >&2
	exit 1
	;;
esac
