#!/usr/bin/env bash
# The consumer's side of the rehearsal bargain.
#
#	check_pin_rehearsal.sh [--go-mod FILE] [--receipt FILE] [--previous-release REF]
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

go_mod="go.mod"
receipt="${REHEARSE_RECEIPT:-}"
previous="${REHEARSE_PREVIOUS_RELEASE:-}"
while [ $# -gt 0 ]; do
	case "$1" in
	--go-mod) [ $# -ge 2 ] || die 2 "--go-mod wants a file"; go_mod="$2"; shift 2 ;;
	--receipt) [ $# -ge 2 ] || die 2 "--receipt wants a file"; receipt="$2"; shift 2 ;;
	--previous-release) [ $# -ge 2 ] || die 2 "--previous-release wants a revision"; previous="$2"; shift 2 ;;
	-h | --help) sed -n '2,26p' "$0"; exit 0 ;;
	*) die 2 "unknown argument $1; --help lists them" ;;
	esac
done

[ -r "$go_mod" ] || die 2 "$go_mod is not a readable file"

# The pin, exactly as the caller's go.mod spells it: the require line, not a
# replace, because a replaced module is the consumer's own tree and the rehearsal
# it owes is the one it runs on that tree.
version=$(sed -n 's|^[[:space:]]*\(require[[:space:]]\{1,\}\)\{0,1\}[[:space:]]*github\.com/septagon-oss/platformkit[[:space:]]\{1,\}\(v[^[:space:]/]*\).*|\2|p' "$go_mod" | head -1)
if [ -z "$version" ]; then
	echo "pin-rehearsal: $go_mod requires no github.com/septagon-oss/platformkit, so there is no pin to rehearse."
	exit 0
fi

# Which revision that version is. The module cache records what the proxy said
# when it served the version, and a proxy that resolved a tag records the commit:
# @v/<version>.info's Origin.Hash is the pin's hash. REHEARSE_PIN_REVISION is the
# override for a build that resolved the pin another way (a private mirror, an
# offline copy); naming the revision by hand is allowed, and it is named.
want="${REHEARSE_PIN_REVISION:-}"
if [ -z "$want" ]; then
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
