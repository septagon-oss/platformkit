#!/usr/bin/env bash
# The four verdicts scripts/check_pin_rehearsal.sh can give, on a fixture tree:
# a matching receipt passes, and each of the three refusals is its own case. The
# pin's commit is given by REHEARSE_PIN_REVISION rather than read from the module
# cache, so these cases run offline and answer the same questions either way.
set -uo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
check="$root/scripts/check_pin_rehearsal.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
fails=0

mkdir -p "$work/consumer"
printf 'module example.com/consumer\n\ngo 1.25\n\nrequire github.com/septagon-oss/platformkit v1.2.0\n' \
	>"$work/consumer/go.mod"
printf 'module example.com/consumer\n\ngo 1.2.5\n' >"$work/consumer/plain.mod"

# $1 = expected exit, $2 = what the case claims, then the script's arguments.
run() {
	local want="$1" label="$2"; shift 2
	local out status
	out=$(cd "$work/consumer" && REHEARSE_PIN_REVISION=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa \
		"$check" "$@" 2>&1)
	status=$?
	if [ "$status" != "$want" ]; then
		echo "FAIL  $label: exit $status, want $want"
		printf '%s\n' "$out" | sed 's/^/      /'
		fails=$((fails + 1))
	elif [ "$want" = 0 ] && ! printf '%s' "$out" | grep -Eq 'ok|no pin to rehearse'; then
		echo "FAIL  $label: exit 0 without saying why"
		fails=$((fails + 1))
	else
		echo "ok    $label"
	fi
}

receipt() { jq -n --arg c "$1" --arg b "$2" '{base_ref:$b, candidate_commit:$c, files:[{owner:"audit",version:"35",name:"000035_audit_context.up.sql",phase:"expand",seconds:0.002}], max_lock_ms:0, exit:0}' >"$work/$3"; }
receipt aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa v1.1.0 good.json
receipt bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb v1.1.0 other.json
receipt aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa HEAD wrongbase.json
receipt aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa v1.1.0 failed.json >/dev/null &&
	jq '.exit = 1' "$work/failed.json" >"$work/failed2.json" && mv "$work/failed2.json" "$work/failed.json"

run 1 "a pin with no receipt at all is refused" --receipt "$work/missing.json"
run 1 "a receipt taken for another revision" --receipt "$work/other.json"
run 1 "a receipt not taken against a release" --receipt "$work/wrongbase.json"
run 1 "a receipt whose rehearsal did not pass" --receipt "$work/failed.json"
run 0 "the pin this receipt was taken for" --receipt "$work/good.json"
run 0 "a consumer that pins nothing" --go-mod "$work/consumer/plain.mod" --receipt "$work/missing.json"
run 2 "a tree with no go.mod" --go-mod "$work/consumer/nope.mod" --receipt "$work/good.json"
run 2 "a receipt that is not JSON" --receipt "$root/scripts/check_pin_rehearsal_test.sh"
run 1 "a base that is not the release named" --receipt "$work/good.json" --previous-release v1.0.0
run 0 "the release the receipt names" --receipt "$work/good.json" --previous-release v1.1.0

if [ "$fails" -gt 0 ]; then echo "pin-rehearsal: $fails case(s) failed"; exit 1; fi
echo "pin-rehearsal: every verdict the script can give is a case"
