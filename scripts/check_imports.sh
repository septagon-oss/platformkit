#!/usr/bin/env bash
# Check one repository against its explicitly composed module dependencies:
#   check_imports.sh [repository [dependency-module-path ...]]
# Consumers call this script from their resolved foundation dependency.
# Applications may name constructors; modules consume each other's contracts.
set -euo pipefail

foundation="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(cd "${1:-$foundation}" && pwd)"
if [ "$#" -gt 0 ]; then shift; fi
module_path="$(sed -n 's/^module[[:space:]]*//p' "$root/go.mod")"
[ -n "$module_path" ] || { echo "imports: missing module declaration" >&2; exit 2; }
roots="$module_path${*:+ $*}"
git -C "$root" rev-parse --show-toplevel >/dev/null

# NUL-separated filenames preserve spaces. Test files may import the module
# they exercise; missing tracked files are ignored during a working-tree edit.
present=()
while IFS= read -r -d '' file; do
    case "$file" in *_test.go) continue ;; *.go) ;; *) continue ;; esac
    [ ! -f "$root/$file" ] || present+=("$file")
done < <(git -C "$root" ls-files -z -c -o --exclude-standard -- modules apps)

hits=""
if [ "${#present[@]}" -gt 0 ]; then
    hits="$(
        cd "$root"
        printf '%s\0' "${present[@]}" | xargs -0 -r awk -v roots="$roots" -v self="$module_path" '
            BEGIN { n = split(roots, pkg, " ") }
            {
                for (k = 1; k <= n; k++) {
                    needle = pkg[k] "/modules/"
                    at = index($0, "\"" needle)
                    quote = "\""
                    if (at == 0) { at = index($0, "`" needle); quote = "`" }
                    if (at == 0) continue
                    rest = substr($0, at + 1 + length(needle))
                    end = index(rest, quote)
                    if (end == 0) continue
                    target = path = substr(rest, 1, end - 1)
                    sub(/\/.*/, "", target)
                    sub(/^[^\/]*\/?/, "", path)
                    if (FILENAME ~ /^apps\//) {
                        if (path ~ /^internal(\/|$)/)
                            printf "%s:%d: apps/ reaches into %s/modules/%s/%s\n", FILENAME, FNR, pkg[k], target, path
                        continue
                    }
                    owner = FILENAME
                    sub(/^modules\//, "", owner)
                    sub(/\/.*/, "", owner)
                    if ((pkg[k] == self && target == owner) || path ~ /^contracts(\/|$)/) continue
                    printf "%s:%d: modules/%s imports %s/modules/%s%s\n", FILENAME, FNR, owner, pkg[k], target,
                        (path == "" ? ", its manifest" : "/" path)
                }
            }
        '
    )"
fi
if [ -n "$hits" ]; then
    printf '%s\n' "$hits" >&2
    echo "OUT OF BOUNDS: modules consume contracts; applications compose constructors." >&2
    echo "See the foundation ARCHITECTURE.md for the dependency boundary." >&2
    exit 1
fi
echo "imports: every cross-module import is a contracts/ import"
