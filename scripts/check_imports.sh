#!/usr/bin/env bash
# Gate 6. Fails when a module reaches into another module by any door but its
# contracts/.
#
# A consumer takes an interface declared in the provider's contracts/, and the
# provider's internal/ makes any other coupling a compile error. That is
# ARCHITECTURE.md's third idea, and Go already enforces half of it: an import of
# modules/b/internal from modules/a does not build. It is checked here anyway,
# because a compiler error that says "use of internal package not allowed" is
# not the sentence that explains what the rule is for.
#
# The other half Go does not enforce at all. A module's root package is its
# manifest: it names kit/module, kit/httpx, its own internal/ and every
# dependency of those. A module that imports another module's root package
# therefore takes the whole of somebody else's wiring, compiles fine, and
# quietly makes the two modules one. That is what this gate is really for.
#
# apps/ is the exception in one direction and not the other: composing an
# application means naming every module's root package, and it still may not
# reach into an internal/.
#
# It tests itself first, over fixture files in a temporary directory, because a
# refusal that has never fired is a claim rather than a gate — and the Makefile
# has fourteen targets, so there is nowhere else to hang the test.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
module_path="$(sed -n 's/^module[[:space:]]*//p' "$root/go.mod")"

# gate reads Go file paths on stdin, relative to $1, and prints one line per
# forbidden import. It takes the root as an argument so the self-test below can
# point it at fixtures instead of at this repository.
gate() {
	(cd "$1" && xargs -r awk -v pkg="$module_path" '
		BEGIN { needle = "\"" pkg "/modules/" }
		{
			i = index($0, needle)
			if (i == 0) next
			rest = substr($0, i + length(needle))
			q = index(rest, "\"")
			if (q == 0) next
			# What of which module: "b/contracts" is target b, path contracts.
			target = path = substr(rest, 1, q - 1)
			sub(/\/.*/, "", target)
			sub(/^[^\/]*\/?/, "", path)

			if (FILENAME ~ /^apps\//) {
				if (path ~ /^internal(\/|$)/)
					printf "%s:%d: apps/ reaches into modules/%s/%s\n", FILENAME, FNR, target, path
				next
			}
			owner = FILENAME
			sub(/^modules\//, "", owner)
			sub(/\/.*/, "", owner)
			if (target == owner || path ~ /^contracts(\/|$)/) next
			printf "%s:%d: modules/%s imports %s\n", FILENAME, FNR, owner,
				(path == "" ? "modules/" target ", its manifest" : "modules/" target "/" path)
		}')
}

# selftest proves the refusal fires, on files that are never in this tree: two
# modules that reach for each other, and one that stays inside the rules.
selftest() {
	local dir hits want=3
	dir="$(mktemp -d)"
	trap 'rm -rf "$dir"' RETURN
	mkdir -p "$dir/modules/a/internal" "$dir/modules/a/contracts" "$dir/apps/x"

	# Two refusals: another module's manifest, and another module's internal/.
	printf 'package internal\n\nimport (\n\t"%s/modules/b"\n\t"%s/modules/b/internal"\n)\n' \
		"$module_path" "$module_path" >"$dir/modules/a/internal/bad.go"
	# None: the kernel, another module's contracts/, and its own internal/.
	printf 'package contracts\n\nimport (\n\t"%s/kit/crud"\n\t"%s/modules/b/contracts"\n\t"%s/modules/a/internal"\n)\n' \
		"$module_path" "$module_path" "$module_path" >"$dir/modules/a/contracts/good.go"
	# One: an app composes every module and still may not open one up.
	printf 'package main\n\nimport (\n\t"%s/modules/b"\n\t"%s/modules/b/internal"\n)\n' \
		"$module_path" "$module_path" >"$dir/apps/x/main.go"

	hits="$(printf '%s\n' modules/a/internal/bad.go modules/a/contracts/good.go apps/x/main.go | gate "$dir")"
	if [ "$(printf '%s\n' "$hits" | grep -c .)" -ne "$want" ]; then
		echo "self-test: the gate found" >&2
		printf '%s\n' "$hits" >&2
		echo "self-test: want $want refusals; it does not refuse what it claims to" >&2
		exit 2
	fi
	if printf '%s\n' "$hits" | grep -q 'good.go'; then
		echo "self-test: the gate refused a contracts/ import, which is the one door there is" >&2
		exit 2
	fi
}

selftest

# git ls-files sees new files too, the same way tools/locbudget does. A file it
# still tracks but the working tree has deleted is skipped, which is the state
# between a `rm` and its commit. Test files are excluded: a test may reach for
# whatever it is testing.
files="$(cd "$root" && git ls-files -c -o --exclude-standard -- modules apps |
	grep '\.go$' | grep -v '_test\.go$' || true)"
present=""
while IFS= read -r file; do
	[ -n "$file" ] && [ -f "$root/$file" ] && present="$present$file"$'\n'
done <<<"$files"

hits="$(printf '%s' "$present" | gate "$root")"
if [ -n "$hits" ]; then
	printf '%s\n' "$hits" >&2
	echo "" >&2
	echo "OUT OF BOUNDS: a module may import another module's contracts/, and nothing else." >&2
	echo "A consumer takes an interface from the provider's contracts/. Importing the" >&2
	echo "provider's manifest takes its whole wiring; importing its internal/ takes an" >&2
	echo "implementation. apps/ composes modules, so it names their manifests, and it" >&2
	echo "does not open one up either. See ARCHITECTURE.md, idea 3." >&2
	exit 1
fi

echo "imports: every cross-module import is a contracts/ import"
