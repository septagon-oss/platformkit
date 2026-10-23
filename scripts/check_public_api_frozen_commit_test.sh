#!/usr/bin/env bash
# The release line's frozen commit must be an ancestor of HEAD, or the gate would judge a
# stale branch by its own delta and never by the frozen surface; and the Makefile's gate
# must read the line record and pass no list of accepted changes.
set -euo pipefail
cd "$(dirname "$0")/.."
record=scripts/baselines/public-api-v2.0.0-candidate.json
commit=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["frozen"]["commit"])' "$record")
git merge-base --is-ancestor "$commit" HEAD || { echo "FAIL frozen commit $commit is not an ancestor of HEAD"; exit 1; }
grep -q -- '--line-record \$(APIDIFF_RECORD)' Makefile || { echo "FAIL check-apidiff does not read the line record"; exit 1; }
if grep -n -- '--baseline\|--write-baseline' Makefile scripts/check_public_api.py; then echo "FAIL an accepted-change list option is back"; exit 1; fi
echo "ok   frozen commit is reachable; gate reads the record and takes no list"
