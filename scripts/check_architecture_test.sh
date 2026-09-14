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

# Real go list fixtures prove transitive runtime checks without fetching or
# compiling dependencies. Each package contains only source text and stdlib imports.
packages_repo="$temporary/portable packages"
mkdir -p "$packages_repo/scripts"
cp "$scripts/check_packages.sh" "$packages_repo/scripts/"
printf 'module github.com/septagon-oss/platformkit\n\n' > "$packages_repo/go.mod"
sed -n '/^go[[:space:]]/p' "$scripts/../go.mod" >> "$packages_repo/go.mod"
printf '{"packages":99}\n' > "$packages_repo/packages-budget.json"
# Resolve before forcing local execution: PATH may otherwise contain Go 1.27.
selected_root="$(GOTOOLCHAIN="go$(sed -n 's/^go //p' "$scripts/../go.mod")" go env GOROOT)"
export PATH="$selected_root/bin:$PATH"
for path in apps/platformkit kit/entity kit/locale kit/flags kit/tenancy \
    modules/task/domain design ui/css ui/forms ui/components kit/tenancy/providers/topaz \
    kit/events kit/events/transport kit/events/providers/memory kit/events/providers/nats kit/events/internal/delivery \
    kit/flags/providers/openfeature kit/flags/providers/ofrep kit/locale/providers/xtext \
    kit/db kit/httpx kit/config modules/auth/contracts; do
    mkdir -p "$packages_repo/$path"
    printf 'package fixture\n' > "$packages_repo/$path/fixture.go"
done
packages=(env GOENV=off GOWORK=off GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off bash "$packages_repo/scripts/check_packages.sh")
"${packages[@]}" >/dev/null
foundation=github.com/septagon-oss/platformkit
fixture_import() {
    printf 'package fixture\nimport _ "%s"\n' "$2" > "$packages_repo/$1/fixture.go"
}
boundary_rejects() {
    local owner="$1" dependency="$2" expected="${3:-$1}"
    fixture_import "$owner" "$dependency"
    rejects "$owner reaches $dependency" "$foundation/$expected transitively depends on $dependency" "${packages[@]}"
    printf 'package fixture\n' > "$packages_repo/$owner/fixture.go"
}
boundary_rejects modules/task/domain "$foundation/kit/tenancy"
boundary_rejects kit/entity "$foundation/kit/db"
boundary_rejects design "$foundation/ui/css"
fixture_import kit/tenancy/providers/topaz "$foundation/kit/tenancy"
boundary_rejects kit/tenancy database/sql kit/tenancy/providers/topaz
boundary_rejects kit/tenancy net/http
fixture_import ui/forms "$foundation/ui/components"
boundary_rejects ui/components "$foundation/kit/db" ui/forms
fixture_import kit/events/providers/nats "$foundation/kit/events/internal/delivery"
boundary_rejects kit/events/internal/delivery "$foundation/kit/db" kit/events/providers/nats
fixture_import kit/events/providers/memory "$foundation/kit/events/internal/delivery"
boundary_rejects kit/events/internal/delivery "$foundation/kit/db" kit/events/providers/memory
boundary_rejects kit/events "$foundation/kit/events/providers/nats"
boundary_rejects kit/tenancy/providers/topaz "$foundation/modules/auth/contracts"
boundary_rejects kit/flags/providers/openfeature "$foundation/kit/flags/providers/ofrep"
boundary_rejects kit/flags/providers/ofrep "$foundation/ui/components"
boundary_rejects kit/locale/providers/xtext "$foundation/kit/flags"
"${packages[@]}" >/dev/null
fixture_import kit/tenancy database/sql
rejects 'write mode bypasses portability' 'transitively depends on database/sql' "${packages[@]}" --write
if [[ "$(cat "$packages_repo/packages-budget.json")" != '{"packages":99}' ]]; then
    echo 'FAIL: failed portability check wrote the budget' >&2; exit 1
fi
rm -r "$packages_repo/apps"
rejects 'missing app bypasses portability' 'transitively depends on database/sql' "${packages[@]}"
printf 'package fixture\n' > "$packages_repo/kit/tenancy/fixture.go"
"${packages[@]}" >/dev/null
mkdir -p "$packages_repo/apps/platformkit"
printf 'package fixture\n' > "$packages_repo/apps/platformkit/fixture.go"

# Only error propagation and foreign SDK metadata need a fake go executable.
# It delegates real dependency discovery and insists on Deps, not direct Imports.
real_go="$(command -v go)"
mkdir "$temporary/bin"
cat > "$temporary/bin/go" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
if [[ "$*" == *' -f '* ]]; then
    [[ "$*" == *'{{join .Deps " "}}'* ]] || { echo 'fixture requires transitive Deps' >&2; exit 2; }
    [[ "$FAKE_GO_MODE" != closure-failure ]] || { echo 'fixture closure go list failed' >&2; exit 73; }
    output="$("$REAL_GO" "$@")"
    if [[ "$FAKE_GO_MODE" == missing ]]; then
        printf '%s\n' "$output" | awk -F '|' '$1 != "github.com/septagon-oss/platformkit/kit/entity"'
        exit 0
    fi
    if [[ "$FAKE_GO_MODE" == sdk ]]; then
        printf '%s\n' "$output" | awk -F '|' -v OFS='|' -v owner="$SDK_OWNER" -v dep="$SDK_DEP" -v module="$SDK_MODULE" '
            $1 == owner { $3 = $3 " " dep }
            { print }
            END { print dep, "false", "", module }
        '
        exit 0
    fi
elif [[ "$FAKE_GO_MODE" == app-failure ]]; then
    echo 'fixture app go list failed' >&2; exit 74
fi
exec "$REAL_GO" "$@"
SH
chmod +x "$temporary/bin/go"
fake=(env PATH="$temporary/bin:$PATH" REAL_GO="$real_go")
rejects 'closure go list failure' 'fixture closure go list failed' "${fake[@]}" FAKE_GO_MODE=closure-failure "${packages[@]}"
rejects 'application go list failure' 'fixture app go list failed' "${fake[@]}" FAKE_GO_MODE=app-failure "${packages[@]}"
rejects 'missing owner metadata' 'missing dependency metadata for' "${fake[@]}" FAKE_GO_MODE=missing "${packages[@]}"
rejects 'transitive NATS SDK in SQL outbox' 'transitively depends on github.com/nats-io/nats.go' \
    "${fake[@]}" FAKE_GO_MODE=sdk SDK_OWNER="$foundation/kit/events" SDK_DEP=github.com/nats-io/nats.go SDK_MODULE=github.com/nats-io/nats.go "${packages[@]}"
rejects 'unselected provider SDK family' 'transitively depends on example.test/other-sdk/client' \
    "${fake[@]}" FAKE_GO_MODE=sdk SDK_OWNER="$foundation/kit/flags/providers/openfeature" SDK_DEP=example.test/other-sdk/client SDK_MODULE=example.test/other-sdk "${packages[@]}"
echo 'package boundaries: transitive core/UI/provider rules, missing metadata and go list failures passed'

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

# Local selectors and an earlier test goal must never narrow the fresh gate.
# Dry runs inspect the real Makefile without starting services or running tests.
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p' "$scripts/../go.mod" > "$temporary/go.mod"
test_commands() {
	make --no-print-directory -n -C "$temporary" -f "$scripts/../Makefile" "$@" |
		sed -n '/^go tool gotestsum /s/[[:blank:]]*$//p'
}
fresh="go tool gotestsum --packages='./...' -- -count=1"
focused="go tool gotestsum --watch --packages='./design ./ui/css' -- -run Selected"
if [[ "$(test_commands test)" != "go tool gotestsum  --packages='./...' --" ]]; then
	echo 'FAIL: local tests must use the default Go cache over every package' >&2
	exit 1
fi
for goals in test check 'test check' 'check test'; do
	read -r -a targets <<< "$goals"
	case "$goals" in
		test) expected="$focused" ;;
		check) expected="$fresh" ;;
		'test check') expected="$focused"$'\n'"$fresh" ;;
		'check test') expected="$fresh"$'\n'"$focused" ;;
	esac
	actual="$(test_commands "${targets[@]}" TEST_PACKAGES='./design ./ui/css' TEST_FLAGS='-run Selected' TEST_OPTIONS=--watch)"
	if [[ "$actual" != "$expected" ]]; then
		printf 'FAIL: make %s changed the local or fresh test boundary:\n%s\n' "$goals" "$actual" >&2
		exit 1
	fi
done
echo 'test feedback: local selectors preserve fresh full checks in either goal order'

# An unrelated PATH formatter must not change formatting or hide tool errors.
formatting="$temporary/formatting"
mkdir -p "$formatting/bin"
cp "$temporary/go.mod" "$formatting/go.mod"
printf 'package fixture\n' > "$formatting/source.go"
cat > "$formatting/bin/go" <<'SH'
#!/bin/sh
[ "$GOTOOLCHAIN" = "$EXPECTED_GOTOOLCHAIN" ] || { echo 'wrong Go toolchain' >&2; exit 71; }
[ "${FAIL_GO_ENV:-0}" = 0 ] || { echo 'fixture go env failed' >&2; exit 72; }
exec "$REAL_GO" "$@"
SH
cat > "$formatting/bin/gofmt" <<'SH'
#!/bin/sh
echo 'unexpected PATH formatter' >&2
exit 73
SH
chmod +x "$formatting/bin/go" "$formatting/bin/gofmt"
formatter=(env PATH="$formatting/bin:$PATH" REAL_GO="$(command -v go)"
    EXPECTED_GOTOOLCHAIN="go$(sed -n 's/^go //p' "$formatting/go.mod")")
format_check=(make --no-print-directory -C "$formatting" -f "$scripts/../Makefile" fmt-check)
if [[ "$("${formatter[@]}" "${format_check[@]}" 2>&1)" != 'gofmt clean' ]]; then
    echo 'FAIL: formatting did not use the selected Go toolchain' >&2
    exit 1
fi
printf 'package fixture\nfunc' > "$formatting/bad.go"
rejects 'formatter syntax error' 'bad.go' "${formatter[@]}" "${format_check[@]}"
rm "$formatting/bad.go"
rejects 'toolchain discovery failure' 'fixture go env failed' "${formatter[@]}" FAIL_GO_ENV=1 "${format_check[@]}"
echo 'formatting: selected toolchain and formatter errors passed'
