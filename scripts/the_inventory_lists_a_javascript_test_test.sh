#!/usr/bin/env bash
# The inventory is one row per test in the tree, whatever runs it: a JavaScript
# or TypeScript test (`*.test.js`, `*.test.mjs`, `*.test.ts`) is a test the
# editor job runs, so it gets a row, its round-named sibling counts against the
# round-named ceiling, and a table that lacks the row is refused.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid
export GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid
export GOWORK=off GOFLAGS=-mod=mod

module="$(sed -n 's/^module //p' "$root/go.mod")"
mkdir -p "$fixture"/{value,tools/editor/browser,tests}
printf 'module %s\n\ngo %s\n' "$module" "$(sed -n 's/^go //p' "$root/go.mod")" > "$fixture/go.mod"
printf 'package value\n' > "$fixture/value/value.go"
cat > "$fixture/value/value_test.go" <<'GO'
package value

import "testing"

func TestValue(t *testing.T) {}
GO
printf '{"name":"editor","type":"module","scripts":{"test":"node --test"}}\n' > "$fixture/tools/editor/package.json"
cat > "$fixture/tools/editor/export.test.mjs" <<'JS'
import test from 'node:test';
import assert from 'node:assert/strict';

test('an export names its frame', () => {
  assert.equal(1 + 1, 2);
});
JS
cat > "$fixture/tools/editor/browser/review_round1_authored_role_read.test.mjs" <<'JS'
import test from 'node:test';
import assert from 'node:assert/strict';

test('an authored role is read back', () => {
  assert.equal('author', 'author');
});
JS
git -C "$fixture" init -q
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: one Go test, two JavaScript tests, one of them round-named'

fails=0
python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --write --ceiling 1 >/dev/null 2>&1 || true

has_row() {
    python3 - "$fixture/tests/inventory.json" "$1" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
sys.exit(0 if any(r["file"] == sys.argv[2] for r in doc["rows"]) else 1)
PY
}
if ! has_row tools/editor/export.test.mjs; then
    echo 'FAIL: --write wrote no row for tools/editor/export.test.mjs, a test the editor job runs' >&2
    fails=$((fails + 1))
fi
if ! has_row tools/editor/browser/review_round1_authored_role_read.test.mjs; then
    echo 'FAIL: --write wrote no row for the round-named JavaScript test' >&2
    fails=$((fails + 1))
fi
round="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["summary"]["round_named_files"])' "$fixture/tests/inventory.json")"
if [ "$round" != 1 ]; then
    printf 'FAIL: summary.round_named_files = %s; the round-named JavaScript test should count against the ceiling, want 1\n' "$round" >&2
    fails=$((fails + 1))
fi

# A table written before the JavaScript test existed is a table the tree no longer answers.
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the inventory records every test'
cat > "$fixture/tools/editor/frame.test.mjs" <<'JS'
import test from 'node:test';
import assert from 'node:assert/strict';

test('a frame has a width', () => {
  assert.equal(390 > 0, true);
});
JS
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: a JavaScript test arrives with no row'
if python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --check >/dev/null 2>&1; then
    echo 'FAIL: --check passed a tree holding tools/editor/frame.test.mjs with no row for it' >&2
    fails=$((fails + 1))
fi

if [ "$fails" -ne 0 ]; then exit 1; fi
echo 'ok — a JavaScript test has a row, counts against the round-named ceiling when so named, and is refused when it has none'
