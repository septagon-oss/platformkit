#!/usr/bin/env bash
# Build the vendored rich text editor bundle in a directory of its own:
#
#   richtext_bundle_build.sh [output-directory]
#
# The node project under tools/richtext-bundle installs nothing inside the
# repository. Its node_modules would otherwise sit in the working tree, where the
# line budget would read forty-odd packages' own README files as this
# repository's markdown — and where a dependency's documentation would be a
# number somebody has to explain. The build therefore happens in a temporary
# copy of the project, which is also what makes "the committed file equals a
# fresh build" a comparison against a build from nothing rather than against
# whatever was installed here last.
#
# With no argument the bundle and its licence are written where they are served
# from; with one, beside the committed pair, which is how the check reads them.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(dirname "$here")"
project="$repo/tools/richtext-bundle"
out="${1:-$repo/ui/assets/richtext}"

work="$(mktemp -d "${TMPDIR:-/tmp}/platformkit-richtext-bundle.XXXXXX")"
trap 'rm -rf "$work"' EXIT

# Only what the build reads is copied: the manifest, the lock, the bundle's own
# source and the build script. Anything else in the project directory is not an
# input, and a build that reached for it would be reaching for this checkout.
for name in package.json package-lock.json entry.js schema.js build.mjs; do
	cp "$project/$name" "$work/$name"
done

cd "$work"
npm ci --no-audit --no-fund
node build.mjs "$out"
