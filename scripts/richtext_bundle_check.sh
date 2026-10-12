#!/usr/bin/env bash
# Assert the two things the vendored editor bundle promises on every pull request:
#
#   1. it fits — the gzip size of the committed file is at most the ceiling, in
#      every case, with or without a node toolchain present; and
#   2. it is what the build produces — the committed bytes equal a fresh build
#      from the lockfile, so a dependency bump cannot ship unbuilt.
#
# The second arm needs node, npm and the network the install reads, so it runs
# when node is present and says so when it is not. The first never skips: a
# ceiling that only the machine with the toolchain enforces is a suggestion.
set -euo pipefail

ceiling="${1:-153600}"
here="$(cd "$(dirname "$0")" && pwd)"
repo="$(dirname "$here")"
bundle="$repo/ui/assets/vendor/richtext/editor.js"
licence="$repo/ui/assets/vendor/richtext/LICENSE.md"
project="$repo/tools/richtext-bundle"

[ -f "$bundle" ] || { echo "richtext bundle: $bundle is missing; run 'make richtext-bundle'" >&2; exit 1; }

size="$(gzip -9 -c "$bundle" | wc -c | tr -d ' ')"
echo "richtext bundle: $size bytes gzipped against a ceiling of $ceiling"
if [ "$size" -gt "$ceiling" ]; then
	echo "richtext bundle: $size gzipped exceeds the $ceiling byte ceiling; the page pays this on every form that carries a rich text field" >&2
	exit 1
fi

if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
	echo "richtext bundle: rebuild comparison skipped (node or npm absent); the size line above still decided"
	exit 0
fi

# npm ci and not npm install: ci installs exactly what package-lock.json pins and
# fails when the lock and the manifest disagree. The condition is the installed
# tree rather than the directory's existence — npm records what it installed in
# node_modules/.package-lock.json, so an install made before a dependency change
# still leaves a node_modules behind and the new pins would go on being ignored
# on every machine that had installed once. scripts/e2e.sh explains the same trap.
matches_lock() {
	node -e 'const fs = require("node:fs");
		const packages = path => JSON.parse(fs.readFileSync(path, "utf8")).packages ?? {};
		let installed;
		try { installed = packages("node_modules/.package-lock.json"); } catch { process.exit(1); }
		for (const [name, pinned] of Object.entries(packages("package-lock.json"))) {
			if (!name || pinned.optional) continue;
			if (installed[name]?.version !== pinned.version) {
				console.error(`richtext bundle: ${name} is ${installed[name]?.version ?? "not installed"}, package-lock.json pins ${pinned.version}; reinstalling`);
				process.exit(1);
			}
		}'
}

fresh="$(mktemp -d "${TMPDIR:-/tmp}/platformkit-richtext-bundle.XXXXXX")"
trap 'rm -rf "$fresh"' EXIT
cd "$project"
matches_lock || npm ci --no-audit --no-fund
node build.mjs "$fresh" >/dev/null

# cmp names the file that differs, and a byte comparison is the only verdict
# worth having here: the check is "the committed file is the build", not "the
# committed file behaves like the build", which is what the journeys decide.
for name in editor.js LICENSE.md; do
	if ! cmp -s "$repo/ui/assets/vendor/richtext/$name" "$fresh/$name"; then
		echo "richtext bundle: ui/assets/vendor/richtext/$name differs from a fresh build" >&2
		echo "  committed $(sha256sum "$repo/ui/assets/vendor/richtext/$name" | cut -c1-16)" >&2
		echo "  rebuilt   $(sha256sum "$fresh/$name" | cut -c1-16)" >&2
		echo "  run 'make richtext-bundle' and commit the result" >&2
		exit 1
	fi
done
echo "richtext bundle: the committed file equals a fresh build from the lockfile"
