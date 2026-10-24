#!/usr/bin/env bash
# The consumer's side of the rehearsal bargain.
#
#	check_pin_rehearsal.sh [--go-mod FILE] [--receipt FILE] [--previous-release REF]
#	                             [--strict]
#
# scripts/rehearse_migrations.sh answers "what will this release's migrations
# cost on a table the size the installation actually has", and ADR 0011 makes it
# the step a version may not be published without. A *consumer* that pins
# github.com/septagon-oss/platformkit runs this module's migrations against its
# own data, on the day it bumps the pin — and that is exactly the moment the
# rehearsal was for. A bump that skips it is the release nobody measured.
#
# So the step writes a receipt (REHEARSE_RECEIPT, exit 0 only, never on a
# failure), and this script refuses a pin that cannot point at one. It is written
# to be called from a consumer's CI the way scripts/check_imports.sh already is,
# from the foundation dependency the caller resolved; scripts/PIN-REHEARSAL.md is
# the document that says how, and the same repository's own CI calls it here so
# the refusal is exercised on the same code path a consumer runs.
#
# `--strict` is the release mode, and it reads the other document the rehearsal can
# write: the matrix receipt `scripts/rehearse_migrations.sh --supported` leaves behind,
# with one case per source the release promises to upgrade from. Which sources those
# are comes from scripts/supported_sources.sh — the same record the rehearsal ran,
# read again here on the checker's own authority — so a receipt can never decide for
# itself which cases it owed. A matrix that is missing its oldest installation, its
# shipped-000048 revision or its later installed patch is refused even when every case
# inside it passed, and a legacy single-case receipt is refused outright rather than
# read as a matrix of one.
#
# Exit codes, because a gate that cannot tell its failures apart teaches nothing:
#
#	0  the pin has a rehearsal, and it is this pin's
#	1  the refusal: no receipt, a receipt for another revision, or a receipt
#	   that was not taken against a release
#	2  the check itself could not run: no go.mod, no such pin, no module cache,
#	   a receipt that is not the JSON this step writes
#
# There is no --skip, and none will be added. A gate with a flag that turns it
# off is a note, not a gate.
set -euo pipefail

die() { echo "pin-rehearsal: $*" >&2; exit "${1:?}"; }

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
record="$root/scripts/testdata/rehearse/supported-sources.json"
reader="$root/scripts/supported_sources.sh"
go_mod="go.mod"
receipt="${REHEARSE_RECEIPT:-}"
previous="${REHEARSE_PREVIOUS_RELEASE:-}"
strict=0
target_from_checkout=0
while [ $# -gt 0 ]; do
	case "$1" in
	--go-mod) [ $# -ge 2 ] || die 2 "--go-mod wants a file"; go_mod="$2"; shift 2 ;;
	--receipt) [ $# -ge 2 ] || die 2 "--receipt wants a file"; receipt="$2"; shift 2 ;;
	--previous-release) [ $# -ge 2 ] || die 2 "--previous-release wants a revision"; previous="$2"; shift 2 ;;
	--strict) strict=1; shift ;;
	-h | --help) sed -n '2,34p' "$0"; exit 0 ;;
	*) die 2 "unknown argument $1; --help lists them" ;;
	esac
done

[ -r "$go_mod" ] || die 2 "$go_mod is not a readable file"

# The pin, exactly as the caller's go.mod spells it: the require line, not a
# replace, because a replaced module is the consumer's own tree and the rehearsal
# it owes is the one it runs on that tree.
version=$(sed -n 's|^[[:space:]]*\(require[[:space:]]\{1,\}\)\{0,1\}[[:space:]]*github\.com/septagon-oss/platformkit[[:space:]]\{1,\}\(v[^[:space:]/]*\).*|\2|p' "$go_mod" | head -1)
if [ -z "$version" ]; then
	if [ "$strict" != 1 ]; then
		echo "pin-rehearsal: $go_mod requires no github.com/septagon-oss/platformkit, so there is no pin to rehearse."
		exit 0
	fi
	# The kernel has no pin of its own, which used to be the sentence that ended the
	# check. In release mode it cannot: the tree the scripts live in *is* the revision
	# being released, so the target is that commit, and the matrix the release owes is
	# owed to it. A consumer whose go.mod names no pin is unaffected — it is strict mode
	# that asks for the kernel's record, and this repository is the only tree holding it.
	target_root=$(git -C "$root" rev-parse HEAD 2>/dev/null) ||
		die 2 "$go_mod names no pin and the tree at $root names no commit to release"
	version="(the checkout at $root)"
	want="$target_root"
	target_from_checkout=1
fi

# Which revision that version is. The module cache records what the proxy said
# when it served the version, and a proxy that resolved a tag records the commit:
# @v/<version>.info's Origin.Hash is the pin's hash. REHEARSE_PIN_REVISION is the
# override for a build that resolved the pin another way (a private mirror, an
# offline copy); naming the revision by hand is allowed, and it is named.
want="${want:-${REHEARSE_PIN_REVISION:-}}"
if [ -z "$want" ] && [ "$target_from_checkout" != 1 ]; then
	gomodcache=$(go env GOMODCACHE 2>/dev/null) || die 2 "go env GOMODCACHE failed"
	escaped=$(printf '%s' github.com/septagon-oss/platformkit | sed 's/\([A-Z]\)/!\l\1/g')
	info="$gomodcache/$escaped@v/$version.info"
	if [ -r "$info" ]; then
		want=$(jq -r '.Origin.Hash // empty' "$info")
	fi
fi
if [ -z "$want" ]; then
	echo "pin-rehearsal: the pin is $version and its commit cannot be resolved from the module cache ($gomodcache/$escaped@v/$version.info carries no Origin.Hash)."
	echo "pin-rehearsal: run the rehearsal against that revision and export REHEARSE_PIN_REVISION=<commit>, or resolve the pin through a proxy that records Origin."
	exit 2
fi

if [ -z "$receipt" ]; then
	echo "pin-rehearsal: $go_mod pins $version, commit ${want:0:12}, and no rehearsal receipt was named."
	echo "pin-rehearsal: set REHEARSE_RECEIPT to the file this step writes; scripts/PIN-REHEARSAL.md is how."
	exit 1
fi
if [ ! -r "$receipt" ]; then
	echo "pin-rehearsal: the receipt named at $receipt cannot be read, and a pin whose rehearsal left no receipt is a pin that skipped it."
	exit 1
fi
jq -e . "$receipt" >/dev/null 2>&1 || die 2 "$receipt is not JSON the rehearsal step writes"

# The matrix receipt, if that is what was handed in. Which cases it must hold comes from
# the support record this script reads for itself, never out of the receipt, so narrowing
# the matrix is a change to the record and not a new baseline. A receipt that measured one
# source is refused rather than read as a matrix of one.
record_kind=$(jq -r '.record // "platformkit.rehearsal.v1"' "$receipt")
if [ "$record_kind" = "platformkit.rehearsal.v2" ]; then
	[ -r "$reader" ] || die 2 "the support record's reader at $reader is not readable, so no case of this matrix can be checked"
	jq -e '.exit == 0' "$receipt" >/dev/null ||
		{ echo "pin-rehearsal: the matrix receipt records exit $(jq -r '.exit' "$receipt"); the step writes one only where every case passed."; exit 1; }
	[ "$(jq -r '.target_commit // empty' "$receipt")" = "$want" ] ||
		{ echo "pin-rehearsal: the matrix was taken into $(jq -r '.target_commit // "nothing"' "$receipt"), and the target is ${want:0:12} ($version)."; exit 1; }
	[ "$(jq -r '.support_record.sha256 // empty' "$receipt")" = "$(sha256sum "$record" | cut -d' ' -f1)" ] ||
		{ echo "pin-rehearsal: the matrix names a support record other than the one this release ships at ${record#"$root"/}."; exit 1; }
	required=$(mktemp)
	"$reader" positives >"$required" ||
		{ echo "pin-rehearsal: the support record will not say which sources this release must upgrade from: $("$reader" positives 2>&1 | head -1)"; exit 1; }
	while IFS=$'\t' read -r src_ref src_commit src_roles; do
		[ -n "$src_ref" ] || continue
		hits=$(jq -r --arg ref "$src_ref" --arg commit "$src_commit" \
			'[.sources[] | select(.ref == $ref and .commit == $commit)] | length' "$receipt")
		[ "$hits" = "1" ] || {
			echo "pin-rehearsal: the matrix holds $hits result(s) for $src_ref (${src_commit:0:12}), which discharges ${src_roles:-a role}; the record requires exactly one case of it."
			exit 1
		}
		case_evidence=$(jq -r --arg ref "$src_ref" --arg commit "$src_commit" '
			(.sources[] | select(.ref == $ref and .commit == $commit) | .rehearsal) // {}
			| [ (.base_commit // ""), (.candidate_commit // ""), (.exit // -1), ((.files // []) | length) ] | @tsv' "$receipt")
		IFS=$'\t' read -r case_base case_candidate case_status case_files <<EOF
$case_evidence
EOF
		[ "$case_base" = "$src_commit" ] ||
			{ echo "pin-rehearsal: the case for $src_ref reports a base of ${case_base:-nothing}, not ${src_commit:0:12}: the copy was not made from the revision the record names."; exit 1; }
		[ "$case_candidate" = "$want" ] ||
			{ echo "pin-rehearsal: the case for $src_ref was applied to ${case_candidate:-nothing}, not to the target ${want:0:12}."; exit 1; }
		[ "$case_status" = "0" ] ||
			{ echo "pin-rehearsal: the case for $src_ref records exit $case_status; a case that did not pass is not a supported upgrade."; exit 1; }
		[ "$case_files" -gt 0 ] ||
			{ echo "pin-rehearsal: the case for $src_ref applied no file, which against a supported installation means the copy was not the installation."; exit 1; }
	done <"$required"
	cases=$(jq -r '.sources | length' "$receipt")
	needed=$(grep -c '' <"$required")
	[ "$cases" = "$needed" ] ||
		{ echo "pin-rehearsal: the matrix holds $cases case(s) and the support record requires $needed; a repeated case cannot discharge a missing one."; exit 1; }
	refused_in_receipt=$(jq -r '.refused[].ref' "$receipt" | sort)
	refused_in_record=$("$reader" refused | cut -f1 | sort)
	[ "$refused_in_receipt" = "$refused_in_record" ] ||
		{ echo "pin-rehearsal: the matrix refuses [$(printf '%s' "$refused_in_receipt" | tr '\n' ' ')] and the record refuses [$(printf '%s' "$refused_in_record" | tr '\n' ' ')]; a refused source is named in both or in neither."; exit 1; }
	echo "pin-rehearsal: ok — $version (${want:0:12}) upgraded from $(jq -r '.sources | length' "$receipt") supported source(s): $(jq -r '[.sources[].ref] | join(", ")' "$receipt"), with $(jq -r '.refused | length' "$receipt") refusal(s) exercised against the record."
	exit 0
fi
if [ "$strict" = 1 ]; then
	echo "pin-rehearsal: $receipt reports no release matrix. Release mode is certified by what scripts/rehearse_migrations.sh --supported writes, and a release is never published from one case."
	exit 1
fi

have=$(jq -r '.candidate_commit // empty' "$receipt")
base=$(jq -r '.base_ref // empty' "$receipt")
status=$(jq -r '.exit' "$receipt")
if [ -z "$have" ]; then
	die 2 "$receipt carries no candidate_commit, so it says nothing about which revision was rehearsed"
fi

if [ "$have" != "$want" ]; then
	echo "pin-rehearsal: the receipt was taken for ${have:0:12}, and the pin is ${want:0:12} ($version). Rehearse the revision the pin names:"
	echo "  git checkout $want && REHEARSE_RECEIPT=$receipt ./scripts/rehearse_migrations.sh --base-ref ${base:-$previous}"
	exit 1
fi
if [ "$status" != "0" ]; then
	echo "pin-rehearsal: the receipt for ${want:0:12} records exit $status — the step writes a receipt only on the path where it is true."
	exit 1
fi
if [ -z "$base" ] || ! printf '%s' "$base" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "pin-rehearsal: the receipt for ${want:0:12} was taken against '${base:-nothing}', which is not a release tag."
	echo "pin-rehearsal: a rehearsal measures this release against the one installations are running; against HEAD it measures an empty diff."
	exit 1
fi
if [ -n "$previous" ] && [ "$base" != "$previous" ]; then
	echo "pin-rehearsal: the receipt's base is $base; the release being left behind is $previous."
	exit 1
fi

files=$(jq -r '.files | length' "$receipt")
echo "pin-rehearsal: ok — $version (${want:0:12}) rehearsed from $base, $files file(s), max sampled lock wait $(jq -r '.max_lock_ms' "$receipt")ms."
