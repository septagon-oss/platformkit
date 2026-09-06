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
