#!/usr/bin/env bash
# The comparison of two values cuts the words both begin alike and the words both end alike before it
# aligns anything, and each of the two cuts at the end stops at the shorter of the two middles. Those
# two bounds are what keep a cut from reaching behind the stretch the other cut already took: written
# without them, a value that is wholly the beginning of the next one — "draft" against "draft draft",
# which is a person typing the same word twice — leaves the middle slice reversed, and the panel beside
# a record is a panic rather than a page. This case asks, one bound at a time, whether the package is
# held to that bound: the comparison as it ships must pass, and either bound written looser must be
# refused. Everything runs in a throwaway checkout, so the worktree beside this script is only read.
set -euo pipefail

root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
repo="$(cd "$(dirname "$0")/.." && pwd)"
diff_file=ui/screens/history_diff.go
clamps='tail < len(oldWords)-head && tail < len(newWords)-head'
failures=0

loosen() { # loosen <bound written looser>
	python3 - "$root/checkout/$diff_file" "$clamps" "$1" <<'PY'
import sys
path, old, new = sys.argv[1], sys.argv[2], sys.argv[3]
src = open(path).read()
if src.count(old) != 1:
    sys.exit("the bound this case asks about is not the one in the file")
open(path, "w").write(src.replace(old, new))
PY
}

expect_pass() { # expect_pass <label>
	if ! (cd "$root/checkout" && timeout 900 go test ./ui/screens -count=1 > "$root/out" 2>&1); then
		echo "FAIL: $1: the comparison does not pass as written"
		cat "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $1"
	fi
}

expect_refused() { # expect_refused <label>
	(cd "$root/checkout" && timeout 900 go test ./ui/screens -count=1 > "$root/out" 2>&1) && got=0 || got=$?
	if [ "$got" = 0 ]; then
		echo "FAIL: $1: the package accepts a comparison whose two cuts may overlap"
		failures=$((failures + 1))
	elif ! grep -qE '^(--- FAIL|panic:)' "$root/out"; then
		echo "FAIL: $1: exit $got named no refused case"
		cat "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $1 ($(grep -m1 -E '^(--- FAIL|panic:)' "$root/out" | cut -c1-60))"
	fi
}

git clone --quiet --shared --no-checkout "$repo" "$root/checkout" </dev/null
git -C "$root/checkout" checkout --quiet --detach HEAD </dev/null
before="$(sha256sum < "$root/checkout/$diff_file")"
[ "$before" = "$(sha256sum < "$repo/$diff_file")" ] || { echo 'FAIL: the checkout holds another comparison'; exit 1; }

expect_pass 'the comparison as it ships passes its package'
for loosened in \
	'tail < len(oldWords) && tail < len(newWords)-head' \
	'tail < len(oldWords)-head && tail < len(newWords)'; do
	git -C "$root/checkout" checkout --quiet -- .
	loosen "$loosened"
	expect_refused "a bound written $loosened is refused"
	git -C "$root/checkout" checkout --quiet -- .
done

[ "$(sha256sum < "$root/checkout/$diff_file")" = "$before" ] || echo 'FAIL: the throwaway checkout was left changed'
[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
