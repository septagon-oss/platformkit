#!/usr/bin/env bash
# `make check` and `make check-push` each run every GENERATED writer and then ask git, with
# --exit-code, whether the generated files and tests/INVENTORY.md moved; both CI workflows run
# `make check`. `go tool gotestsum` sees none of this, so a prerequisite that left either goal, or a
# diff that lost its exit status, would leave nothing else red. The refusals edit a copy of the
# Makefile, never this checkout's.
set -euo pipefail
cd "$(dirname "$0")/.."
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }

# regenerates_then_diffs <makefile> <goal>: the dry run prints both writers, then the diff, in order.
regenerates_then_diffs() {
	make -n -f "$1" "$2" </dev/null 2>/dev/null | awk '
		$0 == "go run ./apps/platformkit composition -config apps/platformkit/testdata/composition.yaml -dir apps/platformkit" { c = NR }
		$0 == "PYTHONDONTWRITEBYTECODE=1 python3 scripts/inventory_figures.py" && c { i = NR }
		/^git diff --exit-code -- \$\(git ls-files \| git check-attr --stdin linguist-generated .*\) tests\/INVENTORY\.md$/ && i { d = NR }
		END { exit !(c && i && d) }'
}

for goal in check check-push; do
	if regenerates_then_diffs Makefile "$goal"; then
		echo "ok   make $goal runs both writers, then git diff --exit-code over the generated files"
	else
		fail "make $goal does not regenerate and then diff with --exit-code"
	fi
done

for workflow in .gitea/workflows/ci.yml .github/workflows/ci.yml; do
	if grep -Eq '^[[:space:]]+run: make check[[:space:]]*$' "$workflow"; then
		echo "ok   $workflow runs make check"
	else
		fail "$workflow has no step that runs make check"
	fi
done

# The predicate above would notice each way the wiring can be lost, and a copy as such is accepted.
cp Makefile "$root/Makefile"
regenerates_then_diffs "$root/Makefile" check || fail 'an unedited copy of the Makefile is refused'
refused() { # refused <case name> <goal> <sed script>
	sed "$3" Makefile >"$root/Makefile"
	if cmp -s Makefile "$root/Makefile"; then
		fail "$1: the edit matched nothing in the Makefile"
	elif regenerates_then_diffs "$root/Makefile" "$2"; then
		fail "$1: accepted"
	else
		echo "ok   $1 is refused"
	fi
}
refused 'check without check-generated' check 's/^\(check: build\) check-generated /\1 /'
refused 'check-push without check-generated' check-push 's/^\(check-push: build\) check-generated /\1 /'
refused 'a diff that lost --exit-code' check 's/^\(\tgit diff\) --exit-code -- \$\$(git ls-files/\1 -- $$(git ls-files/'
refused 'check-generated without generate' check 's/^\(check-generated:\) generate /\1 /'

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the check goals regenerate before they diff: all cases hold'
