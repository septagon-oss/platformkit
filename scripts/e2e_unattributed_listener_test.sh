#!/usr/bin/env bash
# A successful socket query with no ownership information must not authorize a run.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# Model a restricted socket table: ss succeeds but cannot identify processes.
# Keep node real so the attribution check actually starts its own listener.
ss() { return 0; }
lsof() { return 1; }
export -f ss lsof

status=0
PLATFORMKIT_TEST_ADMIN_URL='postgres://unused:unused@127.0.0.1:1/unused' \
PLATFORMKIT_TEST_DATABASE_URL='postgres://unused:unused@127.0.0.1:1/unused' \
timeout 15 bash "$root/scripts/e2e.sh" surfaces.spec.ts --list \
    >"$work/output" 2>&1 </dev/null || status=$?

if [ "$status" -ne 1 ]; then
    echo "FAIL: unavailable socket attribution must refuse, got exit $status"
    cat "$work/output"
    exit 1
fi
if ! grep -q 'this machine cannot say which process is listening' "$work/output"; then
    echo 'FAIL: the refusal must explain the unavailable attribution'
    cat "$work/output"
    exit 1
fi
if grep -qE 'a database of its own|serving on|Listing tests:|Total: .* tests' "$work/output"; then
    echo 'FAIL: the run advanced without proof of listener ownership'
    cat "$work/output"
    exit 1
fi
echo 'ok   unavailable socket attribution refuses before database creation or browser dispatch'
