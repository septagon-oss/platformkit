#!/usr/bin/env bash
# The pin check refuses a dompurify override that differs from the version the lock resolves.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/scripts" "$tmp/tools/designexport"
cp -r "$root/scripts/baselines" "$tmp/scripts/"
cp "$root/scripts/the_release_line_record_and_the_design_pin_agree_test.sh" "$tmp/scripts/"
cp -r "$root/tools/designexport/openpencil" "$tmp/tools/designexport/"
sed -i 's/"dompurify": *"[^"]*"/"dompurify": "0.0.1"/' "$tmp/tools/designexport/openpencil/package.json"
if bash "$tmp/scripts/the_release_line_record_and_the_design_pin_agree_test.sh" >/dev/null 2>&1; then
  echo "FAIL: a drifted override was accepted" >&2
  exit 1
fi
echo "ok   a drifted override is refused"
