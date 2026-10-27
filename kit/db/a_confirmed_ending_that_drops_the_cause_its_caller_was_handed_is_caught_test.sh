#!/usr/bin/env bash
# The line about the effects an ending dropped names, in its error= attribute, the answer the caller
# was handed. On the confirmed ending — the transaction whose owner had already ended, so database/sql
# had discarded it before our own ROLLBACK was refused as already done — that attribute is the only
# trace of why the ending happened, and the one part of that line no other case in the package reads
# back. This pin proves the case that does has teeth: it copies this checkout, guards the append with
# the ending's own `known` flag, and requires the case to refuse the copy. A pin that stayed green
# while the attribute stopped being written would pin nothing.
#
# The same development PostgreSQL the package's own cases need (PLATFORMKIT_TEST_ADMIN_URL and
# PLATFORMKIT_TEST_DATABASE_URL); it writes nothing outside a temporary copy of the tree.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
copy="$(mktemp -d)"
trap 'rm -rf "$copy"' EXIT

run_case() {
	(cd "$copy" && GOFLAGS=-buildvcs=false timeout 600 go test -count=1 -timeout 5m \
		-run 'TestTheLineAboutDroppedEffectsOfAnEndedOwnerNamesTheCauseItsCallerWasHanded' ./kit/db/) \
		>"$copy/out" 2>&1
}

fail() { echo "FAIL: $1"; sed -n '1,40p' "$copy/out" 2>/dev/null; exit 1; }

untouched() {
	# Tracked files only: this pin is itself an untracked file beside the case it guards.
	git -C "$root" diff --quiet || fail 'this pin wrote the checkout it tests'
	git -C "$root" diff --cached --quiet || fail 'this pin wrote the checkout it tests'
}

tar --exclude=./.git -C "$root" -cf - . | tar -C "$copy" -xf -

# The case is worth what it costs to keep: green over the tree as it stands.
run_case || fail 'the case about the cause an ended owner handed its caller is red over an untouched copy'
grep -q '^ok ' "$copy/out" || fail 'the case about the cause an ended owner handed its caller did not run'
untouched
echo "ok   the case about the cause an ended owner handed its caller passes over an untouched copy"

# Guard the append with the outcome flag, and the confirmed ending stops naming the caller's cause.
python3 - "$copy/kit/db/tx.go" <<'PY' || fail 'the append this pin guards is not the one it read'
import sys
path = sys.argv[1]
text = open(path).read()
guarded = '''	attrs := []any{"effects", dropped, "committed", e.committed, "rolled_back", e.rollbackConfirmed}
	if e.err != nil && !known {
		attrs = append(attrs, "error", e.err)
	}'''
plain = guarded.replace(' && !known', '')
if text.count(plain) != 1:
	sys.exit(1)
open(path, "w").write(text.replace(plain, guarded))
PY
got=0
run_case || got=$?
[ "$got" != 0 ] || fail 'the case passed with the confirmed ending stripped of the cause it names'
grep -q 'does not name error=' "$copy/out" \
	|| fail 'the case passed its own ending but named nothing about the missing cause'
grep -q 'context canceled' "$copy/out" \
	|| fail 'the case passed its own ending but never missed the owner cause in the line'
untouched
echo "ok   the case refuses an ending that drops the cause its caller was handed"
