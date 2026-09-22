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

# The shapes the tenancy gate exists to catch. Each used to walk straight past
# it, because the gate looked for two spellings of one statement and the
# settings can be written in more than two, in a file type it never opened.
# A consumer's go.mod, so no kit/db directory is exempt here.
printf 'module example.test/product\n' > "$repo/go.mod"
refuses() {
	local description="$1" path="$2" content="$3"
	mkdir -p "$repo/$(dirname "$path")"
	printf '%s\n' "$content" > "$repo/$path"
	rejects "$description" 'OUT OF BOUNDS' bash "$scripts/check_gucs.sh" "$repo"
	rm "$repo/$path"
}
refuses 'SET without LOCAL, which outlives the commit' internal/escape.go \
	'var q = "SET platformkit.tenant_id = '"'"'x'"'"'"'
refuses 'setting name parked in a constant' internal/escape.go \
	'const guc = "platformkit.tenant_id"'
refuses 'name assembled at runtime' internal/escape.go \
	'var q = "SELECT set_config('"'"'platformkit'"'"' || '"'"'.tenant_id'"'"', 1, true)"'
refuses 'qualified set_config with odd spacing' internal/escape.go \
	'var q = "SELECT pg_catalog.set_config ( '"'"'platformkit.system_access'"'"', 1, true )"'
refuses 'a default pinned to the role' migrations/escape.sql \
	"ALTER ROLE platformkit_app SET platformkit.tenant_id = 'x';"
refuses 'set_config in a migration' migrations/escape.sql \
	"SELECT set_config('platformkit.system_access', 'true', false);"
refuses 'SET without LOCAL in a migration' migrations/escape.sql \
	"SET platformkit.system_access = 'true';"

# What a migration still has to be allowed to do: the policies in 000001 read
# the setting they are written to match. Refusing this would refuse the
# foundation's own schema, which is how a gate gets switched off.
mkdir -p "$repo/migrations"
printf 'SELECT current_setting(%splatformkit.tenant_id%s, true) IS NOT NULL;\n' "'" "'" > "$repo/migrations/000001_tenancy.up.sql"
bash "$scripts/check_gucs.sh" "$repo" >/dev/null
rm -r "$repo/migrations"

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
for path in apps/platformkit kit/entity kit/entity/display kit/locale kit/fault kit/flags kit/tenancy \
    modules/task/domain design ui/css ui/forms ui/components ui/components/examples ui/document ui/resource ui/page ui/screens ui/export kit/tenancy/providers/topaz \
    kit/app kit/health migrations kit/module kit/jobs kit/crud kit/problem kit/rest \
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
boundary_rejects ui/page "$foundation/ui/export"
boundary_rejects ui/screens "$foundation/ui/export"
# The database-free presentation cores: a document is values and a screen is a
# schema plus rows, so neither may reach the router, the database or a module.
boundary_rejects ui/document "$foundation/kit/httpx"
boundary_rejects ui/resource "$foundation/kit/db"
boundary_rejects kit/entity/display "$foundation/kit/crud"
# The refusal sentinels exist so a value package can name one without linking
# the storage adapter, so the edge this case refuses is the one into a
# transaction: kit/db, and through it gorm and a driver. The edge in the other
# direction — kit/fault reaching kit/crud, which a reader might add to "share"
# the names — is unwriteable here rather than merely forbidden, because kit/crud
# imports kit/fault and the Go compiler refuses the cycle before any gate sees
# it. What protects the package either way is the empty allowance below,
# check("kit/fault", ""), which refuses this closure on its first non-standard line.
boundary_rejects kit/fault "$foundation/kit/db"
# The runner selects a transport by name and builds none.
boundary_rejects kit/app "$foundation/kit/events/providers/nats"
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

# Declared versions are the build: a replace directive in either form and a
# go.work file are refused, a commented-out directive is not.
versions="$temporary/versions"
mkdir "$versions"
printf 'module example.test/versions\n\ngo 1.26\n\nrequire example.test/dep v1.0.0\n// replace example.test/dep => ../dep\n' > "$versions/go.mod"
bash "$scripts/check_versions.sh" "$versions" >/dev/null
printf 'module example.test/versions\n\nreplace example.test/dep => ../dep\n' > "$versions/go.mod"
rejects 'one-line replace directive' 'replaces a dependency' bash "$scripts/check_versions.sh" "$versions"
printf 'module example.test/versions\n\nreplace (\n\texample.test/dep => ../dep\n)\n' > "$versions/go.mod"
rejects 'replace block' 'replaces a dependency' bash "$scripts/check_versions.sh" "$versions"
printf 'module example.test/versions\n' > "$versions/go.mod"
printf 'go 1.26\n\nuse .\n' > "$versions/go.work"
rejects 'workspace file' 'go.work' bash "$scripts/check_versions.sh" "$versions"
rm "$versions/go.work"
rejects 'missing module' 'no go.mod' bash "$scripts/check_versions.sh" "$temporary"
echo 'version gate: replace directives, workspace files and missing modules passed'

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

# The rehearsal is a release step, so the refusals it exists to make have to be
# real and answerable without a database: a step that quietly ran nothing would be
# worse than no step, because the release would read it as a pass. Each case below
# is refused before the script connects to anything, which is what makes them
# checkable here.
rehearse=(bash "$scripts/rehearse_migrations.sh")
rejects 'a rehearsal with no copy to rehearse on' 'say where the copy comes from' "${rehearse[@]}"
rejects 'a rehearsal with two copies at once' 'are alternatives' "${rehearse[@]}" --dump x --base-ref HEAD
rejects 'a rehearsal whose dump is not there' 'no dump readable at' "${rehearse[@]}" --dump "$temporary/nope.dump"
rejects 'a budget that is not a number of seconds' 'not a number of seconds' "${rehearse[@]}" --base-ref HEAD --max-file-seconds soon
rejects 'a budget that is not a number of milliseconds' 'not a number of milliseconds' "${rehearse[@]}" --base-ref HEAD --max-lock-ms 1.5
rejects 'an argument the step does not have' 'unknown argument' "${rehearse[@]}" --rollback
rejects 'no owner connection to create a database with' 'PLATFORMKIT_TEST_ADMIN_URL is unset' \
	env -u PLATFORMKIT_TEST_ADMIN_URL bash "$scripts/rehearse_migrations.sh" --base-ref HEAD
rejects 'a base revision that is not here' 'does not name a revision' "${rehearse[@]}" --base-ref no-such-revision
echo 'rehearsal step: bad arguments and a missing owner connection are refused before a database is touched'

# Two of the numbers this step reports are only worth what they say if the program
# behind them runs, and both were silently broken once. Neither needs a database to
# check, because the program is text in the step and the input is a log line the runner
# really wrote and a file the step really writes.
rehearse_script="$scripts/rehearse_migrations.sh"

# 1. The contention grep. A POSIX bracket reads `[^\n]` as "not a backslash and not the
# letter n", so the pattern this step first carried could not span the n inside "the
# migration is contended": the CONTENDED branch never ran, the finding count stayed 0
# and a contended release exited 1 ("a migration failed") from the one step whose job
# is telling those two apart. The program is read out of the step rather than restated,
# so the case cannot pass by agreeing with itself.
contention=$(sed -n "s/^contended=\\\$(grep -o '\\(.*\\)'.*/\\1/p" "$rehearse_script")
if [ -z "$contention" ]; then
	echo 'FAIL: the rehearsal no longer greps the candidate log for a contended file at all; this case has to be rewritten to say what it does instead' >&2
	exit 1
fi
cat >"$temporary/rehearse-run.log" <<'LOG'
{"time":"2026-09-22T13:13:44+01:00","level":"INFO","msg":"db: applied migration","owner":"platformkit","version":21,"name":"000021_limits.up.sql","phase":"expand","duration_ms":3}
platformkit: db: migrate: platformkit/000099_review_probe.up.sql: db: migration is contended: it could not take a lock within its budget; nothing this run had not already applied was applied, and it may be run again (lock_timeout 5s, statement_timeout 0): ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)
LOG
if ! grep -q "$contention" "$temporary/rehearse-run.log"; then
	printf 'FAIL: the contention program %s matches nothing in the line the runner writes for a contended file: CONTENDED never prints, the finding count stays 0, and a contended release exits 1 rather than 3\n' "$contention" >&2
	exit 1
fi

# 2. The lock-wait watcher. `\watch` repeats the query *buffer*, and a query handed to
# `-c` is gone from the buffer by the time the next `-c` runs — psql 18 answers the
# query once and then "\watch cannot be used with an empty query" — so the sample file
# holds one line however long the run took, `lock_ms` could never pass `--max-lock-ms`,
# and the step reported "0 sample(s) ~ 0ms" of a migration that had really waited five
# seconds on a table lock. The step writes the query and its `\watch` into one file
# psql reads with -f; these lines run the step's own lines that build that file.
watch_program=$(awk '/^watch_sql=/{f=1} f{print} /^\t"\$SAMPLE_S" >"\$watch_sql"$/{f=0}' "$rehearse_script")
if [ -z "$watch_program" ]; then
	echo 'FAIL: the rehearsal no longer writes a query and a \watch into one file; this case has to be rewritten to say what it does instead' >&2
	exit 1
fi
# The fixture runs the step's own lines — its SAMPLE_MS, its own derivation of the
# interval from it, and the printf that builds the file — under the step's own variable
# names. Nothing here restates the step: change how the step builds the file and this
# runs the new way, and passes or fails the way the step then does.
{
	printf 'work=%s\nAPPNAME=%s\n' "$(printf '%q' "$temporary")" "$(printf '%q' platformkit-rehearse)"
	sed -n '/^SAMPLE_MS=/p; /^SAMPLE_S=/p' "$rehearse_script"
	printf '%s\n' "$watch_program"
} >"$temporary/build-watch.sh"
bash "$temporary/build-watch.sh"
if ! grep -q 'pg_stat_activity' "$temporary/watch.sql"; then
	printf 'FAIL: the file the step has psql watch holds no query:\n%s\n' "$watch_program" >&2
	exit 1
fi
interval=$(sed -n 's/^\\watch //p' "$temporary/watch.sql")
if [ -z "$interval" ]; then
	echo 'FAIL: the file the step has psql watch holds no \watch line, so it samples once and the lock-wait finding can never fire' >&2
	exit 1
fi
# The interval has to be SAMPLE_MS expressed in seconds: `samples × SAMPLE_MS` is the
# length of the waits counted only if the interval waited on is the one multiplied by.
sample_ms=$(sed -n 's/^SAMPLE_MS=\([0-9]*\)$/\1/p' "$rehearse_script")
if ! awk -v interval="$interval" -v ms="$sample_ms" 'BEGIN { exit !(interval * 1000 > ms - 1 && interval * 1000 < ms + 1) }'; then
	echo "FAIL: the watcher samples every ${interval}s and the step reports each sample as ${sample_ms}ms; one interval written twice has to agree with itself" >&2
	exit 1
fi
watch_line=$(grep -m1 -F -- '-At -o "$waits" -f "$watch_sql"' "$rehearse_script")
if [ -z "$watch_line" ]; then
	echo 'FAIL: the watcher no longer reads its query and \watch from the file the step builds; a query given to -c is gone from the buffer before \watch repeats it, and the sample file then holds one line for a run of any length' >&2
	exit 1
fi
echo 'rehearsal step: the contended grep matches the runner'\''s line, and the lock-wait watcher holds a query and a \watch psql reads from one file'

# 3. The floor is a window, not a wall clock. The step declares a run unmeasured when
# its sample file holds fewer lines than the window the watcher was alive for resolves
# to, and both halves of that sentence failed once: the floor was built from the
# candidate's seconds rounded up, so a clean run of 55ms that straddled a second
# boundary was asked for five samples its watcher was never alive to take and the step
# reported a passed release as exit 2; and a floor that never fires is the reported 0
# the same finding was about. The step's own function answers both, so run it.
watch_program=$(awk '/^watch_floor\(\)/{f=1} f{print} /^}$/{f=0}' "$rehearse_script")
if [ -z "$watch_program" ]; then
	echo 'FAIL: the rehearsal no longer derives its watch floor from a function; this case has to be rewritten to say what it does instead' >&2
	exit 1
fi
{
	sed -n '/^SAMPLE_MS=/p; /^WATCH_STARTUP_GRACE_MS=/p' "$rehearse_script"
	printf '%s\n' "$watch_program"
} >"$temporary/watch-floor.sh"
floor_from_fixture() {
	bash -c 'source "$1"; watch_floor "$2"' _ "$temporary/watch-floor.sh" "$1"
}
# A short run — two files, 55 to 75 milliseconds, which is what a clean rehearsal of
# this repository's pending files measures — has no floor: nothing was measured wrongly
# about it, and a step that guesses a failure from a run too quick to sample turns a
# passing release red.
floor_short=$(floor_from_fixture 350)
if [ "$floor_short" != 0 ]; then
	printf 'FAIL: the watch floor for a 350ms window is %s, not 0; a clean sub-second rehearsal would be reported as LOCK WATCH BROKEN and exit 2\n' "$floor_short" >&2
	exit 1
fi
# A long run has one, well above the single line a watcher that never repeated its
# query leaves behind, and it grows with the window rather than rounding to seconds.
floor_long=$(floor_from_fixture 20000)
if [ "$floor_long" -lt 20 ]; then
	printf 'FAIL: the watch floor for a 20s window is %s; a watcher that sampled once leaves one line, and a floor under 20 is a floor a dead watcher passes\n' "$floor_long" >&2
	exit 1
fi
if [ "$(floor_from_fixture 60000)" -le "$floor_long" ]; then
	echo 'FAIL: the watch floor does not grow with the window it is given, so it is a constant dressed as a measurement' >&2
	exit 1
fi

# 4. Whether \watch fills the file at all. Everything the step says about lock waits is
# the product of this file's line count, and the reviewer's own case for it — case 2 of
# scripts/review_rehearsal_test.sh — runs a `psql -c <query> -c '\watch 0.1'` invocation
# the step does not use and no change to this repository can make sample: psql answers
# that one once and then "\watch cannot be used with an empty query", whatever it is
# pointed at (measured on psql 18.6 and on the 16.15 inside the project's own image).
# This runs the step's own line, read out of the step, for two seconds, and asks the
# question the reviewer's case asks.
if ! command -v psql >/dev/null || [ -z "${PLATFORMKIT_TEST_ADMIN_URL-}" ]; then
	echo 'rehearsal step: the live watcher needs psql and PLATFORMKIT_TEST_ADMIN_URL; SKIPPED, so the sampling is unproven on this machine'
else
	printf 'run_url=%s\nwaits=%s\nwatch_sql=%s\n' \
		"$(printf '%q' "$PLATFORMKIT_TEST_ADMIN_URL")" \
		"$(printf '%q' "$temporary/waits")" "$(printf '%q' "$temporary/watch.sql")" \
		>"$temporary/watch-live.sh"
	printf '%s\n' "$watch_line" >>"$temporary/watch-live.sh"
	printf 'watcher=$!\nsleep 2\nkill "$watcher" 2>/dev/null || true\nwait "$watcher" 2>/dev/null || true\ngrep -c "" <"$waits" 2>/dev/null || true\n' \
		>>"$temporary/watch-live.sh"
	: >"$temporary/waits"
	watch_samples=$(bash "$temporary/watch-live.sh")
	if [ "${watch_samples:-0}" -lt 4 ]; then
		printf "FAIL: two seconds of the step's own watcher (%s) put %s line(s) in the file it counts; four is what two seconds at %sms resolves to, and a file that holds one line measures no wait of any length\n" "$watch_line" "${watch_samples:-0}" "$sample_ms" >&2
		exit 1
	fi
	echo "rehearsal step: the watch floor is the window the watcher lived for, and its own psql line filled the sample file $watch_samples times in 2s"
fi

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
# The composition gate. A layer may name a thing and style it in its own sheet, or
# ask the foundation for a token; it may not paint with a utility class, and it may
# not emit a class on its own that no rule answers.
uilayers_repo="$temporary/uilayers"
mkdir -p "$uilayers_repo/modules/planner/internal/ui" "$uilayers_repo/apps/shop"
cat > "$uilayers_repo/modules/planner/internal/ui/render.go" <<'GO'
package ui

func Board() string { return h.Class("plan-day-board") + h.Class("home-contact home-wrap") }
GO
cat > "$uilayers_repo/modules/planner/internal/ui/styles.go" <<'GO'
package ui

func Styles(s *css.Sheet) {
	s.Select(".plan-day-board", css.Decl("display", css.Literal("grid")))
	s.Select(".home-wrap", css.Decl("max-width", css.Literal("68rem")))
}
GO
printf 'package main

var _ = h.Class(style.New().Bg(style.SurfacePrimary).Compile())
' > "$uilayers_repo/apps/shop/main.go"
uilayers=(bash "$scripts/check_ui_layers.sh" "$uilayers_repo")
"${uilayers[@]}" >/dev/null

printf 'func Board() string { return h.Class("plan-day-board bg-red-500") }
' >> "$uilayers_repo/modules/planner/internal/ui/render.go"
rejects 'a raw utility class' 'RAW UTILITY CLASSES' "${uilayers[@]}"
sed -i 's/ h.Class("plan-day-board bg-red-500")//' "$uilayers_repo/modules/planner/internal/ui/render.go"

# `.plan-day-board` must not be allowed to answer for `.plan-day`: a prefix match
# once reported this gate clean while classes went unstyled.
printf 'func Day() string { return h.Class("plan-day") }
' >> "$uilayers_repo/modules/planner/internal/ui/render.go"
rejects 'a class answered only by a longer selector' 'UNSTYLED CLASSES' "${uilayers[@]}"
sed -i '/func Day() string/d' "$uilayers_repo/modules/planner/internal/ui/render.go"

# A marker riding beside a styled class is a labelled anchor, not a defect; a class
# written alone with nothing behind it is markup claiming a design nobody wrote.
printf 'func Orphan() string { return h.Class("plan-orphan") }
' >> "$uilayers_repo/modules/planner/internal/ui/render.go"
rejects 'a class emitted alone and never styled' 'plan-orphan' "${uilayers[@]}"
sed -i '/func Orphan() string/d' "$uilayers_repo/modules/planner/internal/ui/render.go"
"${uilayers[@]}" >/dev/null
echo 'ui layers: naming, tokens and companion markers accepted; utilities and orphan hooks rejected'

echo 'formatting: selected toolchain and formatter errors passed'
