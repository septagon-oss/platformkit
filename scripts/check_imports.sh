#!/usr/bin/env bash
# Check Go's direct production and test imports in the caller's build context:
#   check_imports.sh [repository [dependency-module-path ...]]
# Build the resolved foundation's tool separately so its exit status survives.
set -euo pipefail

foundation="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(cd "${1:-$foundation}" && pwd)" || exit 2
if [ "$#" -gt 0 ]; then shift; fi
temporary="$(mktemp -d)" || exit 2
trap 'rm -rf "$temporary"' EXIT
trap 'exit 2' HUP INT TERM

if ! (
    cd "$foundation" || exit 2
    export GOWORK=off GOFLAGS=
    host_os="$(go env GOHOSTOS)" || exit 2
    host_arch="$(go env GOHOSTARCH)" || exit 2
    export GOOS="$host_os" GOARCH="$host_arch"
    go build -mod=readonly -buildvcs=false -o "$temporary/importgate" ./tools/importgate
); then
    echo 'imports: could not build the resolved foundation import gate' >&2
    exit 2
fi
"$temporary/importgate" "$root" "$@"
