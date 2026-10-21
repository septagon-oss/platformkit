#!/usr/bin/env bash
# The wire package's test binary links nothing that opens a store, so its compatibility
# cases run at the push tier with no database behind them. The inventory states that
# as needs_db=false from a text search of the test files; this asks the link graph.
# Pass a package path to ask the same question of another package.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
package="${1:-./kit/wire}"
opens_a_store='(^|/)(jackc/pgx|platformkit/kit/db(/|$)|testcontainers|database/sql$|nats-io/nats\.go|redis/go-redis|valkey-io)'

linked="$(cd "$repo_root" && go list -deps -test "$package")"
if opened="$(printf '%s\n' "$linked" | grep -E "$opens_a_store")"; then
  printf '%s: the test binary links a package that opens a store:\n%s\n' "$package" "$opened" >&2
  exit 1
fi
echo "$package: the test binary links no package that opens a store, so its cases run stack-free"
