#!/usr/bin/env bash
# The one reader of the supported-source record.
#
#	supported_sources.sh [--record FILE] (verify | positives | refused | fixtures | verdict REF)
#
# scripts/testdata/rehearse/supported-sources.json says which installations this
# release promises to upgrade: the exact revision each name is, which obligation it
# discharges, and which revisions are refused with a reason. scripts/rehearse_migrations.sh
# asks it what to rehearse and scripts/check_pin_rehearsal.sh asks it what a receipt
# had to cover — both ask the same file, so a receipt can never decide for itself
# which sources were required.
#
# What "supported" *means* is docs/COMPATIBILITY.md and RELEASE.md; this script never
# derives *membership* from a version regex or a tag list, because the policy is not a
# pattern — no tag pattern decides what this release undertakes. It does read the published
# tags and the record's own history for *coverage*: what a record may leave a question off.
# What it checks is the part a record can get wrong silently:
#
#   the shape            a record this script does not understand is not a record
#   a name that is not a revision  every ref must resolve, here, to the commit beside it
#   a proof build called supported  docs/COMPATIBILITY.md gives it no claim to make
#   an undischarged obligation     every required role is discharged exactly once
#   a refusal with no reason       a source is refused by being named, never by being left out
#   an obligation with no reason   every entry says why it is one, and the verdict quotes it
#   a narrowed promise      every installation this repository published or already
#                           promised is answered: supported, or refused with a reason
#
# Exit codes:
#
#	0  the record answers, and the answer is what was asked for
#	1  the refusal: a record that does not hold together, or a source not supported
#	2  the record could not be read at all — no file, no JSON, no git to resolve refs
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# The repository's own record, as a path inside it. The promise floor below is read out of
# this path's committed history even when --record names another file, so a proposed record
# is measured against what the tree already undertakes rather than against itself.
shipped="scripts/testdata/rehearse/supported-sources.json"
record="$root/$shipped"
verb=""
ref=""
while [ $# -gt 0 ]; do
	case "$1" in
	--record) [ $# -ge 2 ] || { echo "supported-sources: --record wants a file" >&2; exit 2; }; record="$2"; shift 2 ;;
	verify | positives | refused | fixtures | verdict) verb="$1"; shift ;;
	-h | --help) sed -n '2,31p' "$0"; exit 0 ;;
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

# The promise may only widen. Which installations a release undertakes to upgrade is not
# a set the diff that certifies it gets to choose: the stable tags this repository
# published are the installations people actually hold, and the record this repository has
# already committed is the promise it made publicly. Every revision in that floor must be
# answered by the record in front of this reader — supported, or refused with a reason —
# because a source that leaves the list without being refused is a promise halved by
# whoever was certifying it, and a record that answers less than its own history promised
# still holds together on every one of the checks above. A release adds an installation in
# this file whenever it likes; it reaches back less far only where the policy documents
# refuse the revision, in prose somebody wrote and this reader cannot write for them.
#
# The floor is the repository's promise rather than the file handed in, which is what lets
# a proposed record be read against what the tree already undertakes.
answered=$(jq -r '[(.sources[] | .ref), (.refused[] | .ref)] | .[]' "$record")
major=$(printf '%s' "$target" | sed -n 's/^v\([0-9]\{1,\}\)\..*/\1/p')
[ -n "$major" ] || refuse "$record names release line $target, which is not a version this reader can read a line out of"

# Two floors, one question. The first is what this repository published: every stable tag
# of the release line this one breaks from names an installation somebody is running, and
# a prerelease or proof tag carries no claim to answer (docs/COMPATIBILITY.md). The second
# is what this repository already undertook: every revision a committed revision of the
# record listed as supported. Neither floor comes from the file in front of this reader,
# which is the whole point — the file in front of this reader is the thing on trial.
published=$(git -C "$root" tag -l "v$((major - 1)).*" 2>/dev/null | grep -E "^v$((major - 1))\.[0-9]+\.[0-9]+$" || true)
promised=""
if [ -e "$root/.git" ]; then
	while IFS= read -r revision; do
		promised="$promised$(git -C "$root" show "$revision:$shipped" 2>/dev/null |
			jq -r '(.sources[]? | .ref) // empty' 2>/dev/null || true)"$'\n'
	done < <(git -C "$root" log --format=%H -- "$shipped" 2>/dev/null || true)
fi
floor=$(printf '%s\n%s\n' "$published" "$promised" | grep -v '^[[:space:]]*$' | sort -u)
while IFS= read -r name; do
	[ -n "$name" ] || continue
	if ! printf '%s\n' "$answered" | grep -qxF "$name"; then
		refuse "$record answers nothing for $name, which this repository has published or already promised as an installation; a revision leaves the supported set by being refused with a reason a person can read, never by being left off a list"
	fi
done <<<"$floor"
nanchored=$(printf '%s\n' "$floor" | grep -c '[^[:space:]]')

# Every obligation states why it is one. A reason no reader reads is a field that only
# looks like configuration, so the two are wired together here: an entry with no reason
# beside it is refused, and the verdict for a supported source prints its reason out loud,
# which is the sentence a release note quotes.
while IFS= read -r name; do
	refuse "$record lists $name as a supported source with no reason stated beside it; every obligation this file takes on says why, because a release note has to be able to quote it"
done < <(jq -r '.sources[] | select((.why // "") == "") | .ref' "$record")

# The bytes an installation already holds applied, and the SQL the rehearsal seeds its
# copies from. Both are named by digest for the same reason: the runner compares a
# migration whole, so a release that rewrites an applied file — a comment is enough —
# is refused by every ledger that file landed in; and a rehearsal whose fixture is
# whatever SQL the current tree happens to hold is measuring a database no installation
# has. Each field is checked here, against the tree the release is built from.
require_digests() { # $1 = record field, $2 = what a missing entry costs the release
	local field="$1" missing="$2" path want have n
	n=$(jq -r --arg f "$field" 'if ((.[$f] | type) == "array") then (.[$f] | length) else 0 end' "$record")
	[ "$n" -gt 0 ] || refuse "$record names no $field; $missing"
	while IFS=$'\t' read -r path want why; do
		[ -n "$path" ] || continue
		[ -n "$why" ] ||
			refuse "$record owes $path with no reason stated beside it; an obligation nobody can explain is a preference, and this file is not where preferences get recorded"
		[ -r "$root/$path" ] ||
			refuse "$record owes the bytes at $path, which this tree does not hold"
		have=$(sha256sum "$root/$path" | cut -d' ' -f1)
		[ "$have" = "$want" ] ||
			refuse "$path is $have here, and $record owes $want: the release rewrites bytes an installation already holds or a fixture the matrix did not run"
	done < <(jq -r --arg f "$field" '.[$f][] | [.path, .sha256 // "", .why // ""] | @tsv' "$record")
	# Set rather than echoed: a caller that read the count out of a command substitution
	# would run `refuse` in a subshell, where its exit stops nothing.
	digests_count=$n
}

require_digests applied_bytes \
	"a supported installation has migrations applied, and the exact bytes it holds are the first thing an upgrade must keep"
nbytes=$digests_count
require_digests fixtures \
	"a rehearsal that seeds whatever its fixture happens to say now certifies a database nobody is running"
nfixtures=$digests_count

case "$verb" in
verify)
	echo "supported-sources: $record — line $target, $nsources supported source(s), $nrefused refused, roles $(jq -r '.required_roles | join(",")' "$record"), $nbytes applied migration byte obligation(s), $nfixtures fixture obligation(s), promise anchor $nanchored installation(s) published or already promised, every one answered"
	;;
positives)
	jq -r '.sources[] | [.ref, .commit, ((.roles // []) | join(","))] | @tsv' "$record"
	;;
refused)
	jq -r '.refused[] | [.ref, .commit // "", .reason] | @tsv' "$record"
	;;
fixtures)
	jq -r '.fixtures[] | [.path, .sha256 // ""] | @tsv' "$record"
	;;
verdict)
	line=$(jq -r --arg ref "$ref" '(.sources[] | select(.ref == $ref) | "supported	" + .commit + "	" + ((.roles // []) | join(",")) + "	" + (.why // "")) // empty' "$record")
	if [ -n "$line" ]; then
		IFS=$'\t' read -r _ commit roles why <<<"$line"
		echo "supported-sources: $ref (${commit:0:12}) is a supported source for $target, discharging ${roles:-no named role} — ${why:-the record states no reason}"
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
