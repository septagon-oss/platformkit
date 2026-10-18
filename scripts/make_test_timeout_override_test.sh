#!/usr/bin/env bash
set -euo pipefail

scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
makefile="${1:-$scripts/../Makefile}"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
sed -n '/^module[[:space:]]/p; /^go[[:space:]]/p; /^toolchain[[:space:]]/p' \
	"$scripts/../go.mod" > "$temporary/go.mod"

test_command() {
	make --no-print-directory -n -C "$temporary" -f "$makefile" \
		PLATFORMKIT_TEST_VALKEY_URL= test "$@" |
		sed -n '/^go tool gotestsum /s/[[:blank:]]*$//p'
}

default="$(test_command)"
if [[ "$default" != "go tool gotestsum  --packages='./...' -- -timeout=30m -p=4" ]]; then
	printf 'FAIL: the local suite lost its stated default timeout: %s\n' "$default" >&2
	exit 1
fi

override="$(test_command TEST_PACKAGES='./design' TEST_FLAGS='-timeout=17s -run Selected')"
if [[ "$override" != "go tool gotestsum  --packages='./design' -- -timeout=30m -p=4 -timeout=17s -run Selected" ]]; then
	printf 'FAIL: the caller timeout must follow the default so Go uses the caller value: %s\n' "$override" >&2
	exit 1
fi

echo 'make test: the full suite has a stated timeout and the caller timeout takes precedence'
