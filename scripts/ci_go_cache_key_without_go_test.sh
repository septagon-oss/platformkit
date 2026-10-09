#!/usr/bin/env bash
# A job whose toolchain cannot be asked where its caches live runs cold and green.
#
# scripts/ci_go_cache_key.sh promises that every refusal writes no `key` output and exits 0, so the
# two cache steps that read it skip and the job is never reddened by its cache. scripts/ci_go_cache_test.sh
# covers a missing go.sum, a missing caller and a caller that is not a slug; this covers the refusal
# the recipe reaches through `go env`: no `go` on PATH at all (setup-go failed, or a step ran before it).
# The recipe runs under `set -eu`, so a failing command substitution that someone later moves out of
# the here-string would turn this refusal into exit 1 — which is what this case would then report.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/bin" "$work/tree"
for tool in bash sha256sum grep sed tr cut; do
	path="$(command -v "$tool")"
	ln -s "$path" "$work/bin/$tool"
done
cp "$root/go.mod" "$root/go.sum" "$work/tree/"
: >"$work/output"

set +e
(cd "$work/tree" && env -i PATH="$work/bin" GITHUB_OUTPUT="$work/output" \
	"$work/bin/bash" "$root/scripts/ci_go_cache_key.sh" check </dev/null >"$work/stdout" 2>"$work/stderr")
code=$?
set -e

if [ "$code" -ne 0 ]; then
	echo "FAIL: with no go on PATH scripts/ci_go_cache_key.sh exits $code; a cache that cannot be named must leave the job green"
	cat "$work/stdout" "$work/stderr"
	exit 1
fi
if grep -q '^key=' "$work/output"; then
	echo "FAIL: with no go on PATH scripts/ci_go_cache_key.sh still wrote a key: $(grep '^key=' "$work/output")"
	exit 1
fi
if ! grep -q '::warning::' "$work/stdout"; then
	echo "FAIL: with no go on PATH scripts/ci_go_cache_key.sh refused without the warning a run's log is read for"
	exit 1
fi
echo "ok   with no go on PATH the cache recipe warns, writes no key and exits 0"
