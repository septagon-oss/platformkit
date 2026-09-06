#!/usr/bin/env bash
# Conformance cases for the shared source gates. All fixture repositories are
# temporary and contain source text only; no dependencies are fetched or built.
set -euo pipefail
scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
repo="$temporary/repository with spaces"
mkdir -p "$repo/modules/a/internal" "$repo/apps/example" "$repo/kit/db/dbtest"
git -C "$repo" init --quiet
printf 'module example.test/product\n' > "$repo/go.mod"

rejects() {
	local description="$1" expected="$2" output
	shift 2
	if output=$("$@" 2>&1); then
		echo "FAIL: $description was accepted" >&2
		exit 1
	fi
	if [[ "$output" != *"$expected"* ]]; then
		printf 'FAIL: %s returned an unrelated error:\n%s\n' "$description" "$output" >&2
		exit 1
	fi
}

imports=(bash "$scripts/check_imports.sh" "$repo" example.test/foundation example.test/catalog)
cat > "$repo/modules/a/internal/good.go" <<'GO'
package internal
import (
    "example.test/product/modules/a/internal"
    "example.test/product/modules/b/contracts"
    "example.test/foundation/modules/a/contracts"
    "example.test/catalog/modules/b/contracts"
)
GO
cat > "$repo/apps/example/main.go" <<'GO'
package main
import "example.test/catalog/modules/b"
GO
"${imports[@]}" >/dev/null

for target in example.test/product/modules/b example.test/foundation/modules/a example.test/catalog/modules/a/internal; do
	printf 'package internal\nimport "%s"\n' "$target" > "$repo/modules/a/internal/bad.go"
	rejects "cross-module import $target" 'OUT OF BOUNDS' "${imports[@]}"
done
printf 'package internal\nimport `example.test/catalog/modules/b`\n' > "$repo/modules/a/internal/bad.go"
rejects 'raw-string constructor import' 'OUT OF BOUNDS' "${imports[@]}"
rm "$repo/modules/a/internal/bad.go"

printf 'package main\nimport "example.test/catalog/modules/b/internal"\n' > "$repo/apps/example/main.go"
rejects 'application imports implementation' 'apps/ reaches into' "${imports[@]}"
printf 'package main\n' > "$repo/apps/example/main.go"
printf 'package internal\nimport "example.test/catalog/modules/b"\n' > "$repo/modules/a/internal/exempt_test.go"
"${imports[@]}" >/dev/null

# Only the actual foundation module grants the direct kit/db exemption. A
# consumer cannot gain it by creating a same-named directory or a subpackage.
printf 'package db\nvar query = "SET LOCAL platformkit.tenant_id = 1"\n' > "$repo/kit/db/write.go"
rejects 'consumer impersonates kit/db' 'OUT OF BOUNDS' bash "$scripts/check_gucs.sh" "$repo"
sed -n '/^module[[:space:]]/p' "$scripts/../go.mod" > "$repo/go.mod"
bash "$scripts/check_gucs.sh" "$repo" >/dev/null
mv "$repo/kit/db/write.go" "$repo/kit/db/dbtest/write.go"
rejects 'foundation subpackage writes tenancy' 'OUT OF BOUNDS' bash "$scripts/check_gucs.sh" "$repo"
rm "$repo/kit/db/dbtest/write.go"

# Tracked files deleted from the working tree do not become scanner failures.
git -C "$repo" add .
rm "$repo/modules/a/internal/good.go"
bash "$scripts/check_gucs.sh" "$repo" >/dev/null
"${imports[@]}" >/dev/null
echo 'architecture gates: dependency boundaries, raw imports, file paths and tenancy ownership passed'

# A push has already advanced main. The previous revision, supplied explicitly,
# must still catch a committed increase instead of comparing main with itself.
budgets="$temporary/budget repository"
mkdir "$budgets"
git -C "$budgets" init --quiet --initial-branch=main
cat > "$budgets/loc-budget.json" <<'JSON'
{"buckets":[{"name":"source","suffixes":[".go"],"max":10},{"name":"tests","suffixes":["_test.go"],"max":12}]}
JSON
printf '{"packages":3}\n' > "$budgets/packages-budget.json"
commit_budget() {
	git -C "$budgets" add .
	git -C "$budgets" -c user.name=Fixture -c user.email=fixture@example.test \
		-c commit.gpgsign=false -c core.hooksPath=/dev/null commit --quiet -m "$1"
}
commit_budget 'Reviewed baseline'
base="$(git -C "$budgets" rev-parse HEAD)"
ratchet=(bash "$scripts/check_budget_ratchet.sh" "$base" "$budgets")
"${ratchet[@]}" >/dev/null
jq '.buckets[0].max = 11' "$budgets/loc-budget.json" > "$temporary/raised.json"
cp "$temporary/raised.json" "$budgets/loc-budget.json"
commit_budget 'Unreviewed increase'
rejects 'committed budget increase' 'source raised from 10 to 11' "${ratchet[@]}"
git -C "$budgets" show "$base:loc-budget.json" > "$budgets/loc-budget.json"
printf '{"packages":4}\n' > "$budgets/packages-budget.json"
rejects 'package ceiling increase' 'raised from 3 to 4' "${ratchet[@]}"
printf '{"packages":"3"}\n' > "$budgets/packages-budget.json"
rejects 'malformed package ceiling' 'packages must be a number' "${ratchet[@]}"
rm "$budgets/packages-budget.json"
rejects 'removed package budget' 'packages-budget.json' "${ratchet[@]}"
printf '{"packages":2}\n' > "$budgets/packages-budget.json"
jq '.buckets[0].max = 9' "$budgets/loc-budget.json" > "$temporary/lowered.json"
cp "$temporary/lowered.json" "$budgets/loc-budget.json"
"${ratchet[@]}" >/dev/null
jq '.buckets |= map(select(.name != "tests"))' "$budgets/loc-budget.json" > "$temporary/removed.json"
cp "$temporary/removed.json" "$budgets/loc-budget.json"
rejects 'removed measurement' 'bucket tests was removed' "${ratchet[@]}"
git -C "$budgets" show "$base:loc-budget.json" > "$budgets/loc-budget.json"
jq '.buckets[0].suffixes = [".ts"]' "$budgets/loc-budget.json" > "$temporary/changed.json"
cp "$temporary/changed.json" "$budgets/loc-budget.json"
rejects 'changed measurement' 'measurement changed for source' "${ratchet[@]}"
printf '{"buckets":' > "$budgets/loc-budget.json"
rejects 'malformed source budget' 'invalid JSON' "${ratchet[@]}"
rejects 'missing base' 'nonzero full base commit SHA' bash "$scripts/check_budget_ratchet.sh" '' "$budgets"
rejects 'new branch without baseline' 'nonzero full base commit SHA' bash "$scripts/check_budget_ratchet.sh" "$(printf '%040d' 0)" "$budgets"
rejects 'unavailable base' 'could not fetch base commit' bash "$scripts/check_budget_ratchet.sh" "$(printf '%040d' 1)" "$budgets"
git -C "$budgets" show "$base:loc-budget.json" > "$budgets/loc-budget.json"
rm "$budgets/packages-budget.json"
commit_budget 'Consumer with no package budget'
bash "$scripts/check_budget_ratchet.sh" "$(git -C "$budgets" rev-parse HEAD)" "$budgets" >/dev/null

# CI begins with only the checked-out revision. Resolve the exact earlier
# commit from the fixture remote, without substituting its current main.
consumer_base="$(git -C "$budgets" rev-parse HEAD)"
jq '.buckets[0].max = 9' "$budgets/loc-budget.json" > "$temporary/lowered.json"
cp "$temporary/lowered.json" "$budgets/loc-budget.json"
commit_budget 'Lowered consumer ceiling'
runner="$temporary/shallow runner"
git init --quiet "$runner"
git -C "$runner" remote add origin "$budgets"
git -C "$runner" fetch --quiet --depth=1 origin main
git -C "$runner" checkout --quiet --detach FETCH_HEAD
if git -C "$runner" cat-file -e "$consumer_base^{commit}" 2>/dev/null; then
	echo 'FAIL: shallow fixture unexpectedly contains its baseline' >&2
	exit 1
fi
bash "$scripts/check_budget_ratchet.sh" "$consumer_base" "$runner" >/dev/null

rm "$budgets/loc-budget.json"
commit_budget 'Missing baseline document'
rejects 'missing source baseline' 'loc-budget.json' bash "$scripts/check_budget_ratchet.sh" "$(git -C "$budgets" rev-parse HEAD)" "$budgets"
echo 'budget ratchet: previous revisions, decreases, removed measurements and missing baselines passed'
