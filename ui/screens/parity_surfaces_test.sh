#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")"

# A filtered run must not silently succeed after either surface fixture disappears.
for fixture in command-owned-create-fields singleton-update-surface; do
    test -f "testdata/parity/$fixture.json"
done

timeout 300 go test . -run '^TestParityFixturesMatchTheProductionResolver/(command-owned-create-fields|singleton-update-surface)$|^TestParityFixtureProvenanceIsEnforced$' -count=1 -v </dev/null
