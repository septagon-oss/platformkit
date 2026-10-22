#!/usr/bin/env bash
# Fails when a production comment cites a review, a round, a finding, a task id
# or a *_test.go file name (decision 0091 D4). tools/commentlint reads the
# comments through go/parser, so strings and directives are not mistaken for
# them; the long blocks and the ratio it also prints are a trend, not a failure.
set -euo pipefail

foundation="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(cd "${1:-$foundation}" && pwd)"
cd "$foundation"
go run ./tools/commentlint "$root" | grep -v '^LONG '
