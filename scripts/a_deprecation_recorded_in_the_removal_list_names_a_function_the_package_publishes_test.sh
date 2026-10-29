#!/usr/bin/env bash
# A `// Deprecated:` comment warns the reader who opens the file; the compatibility record is the
# list the next major reads before it removes anything. kit/rest carries both halves for the
# positional command pair, and nothing re-checks that the two halves still say one thing: a row
# dropped from the record leaves a marker no removal can act on, a row beside a marker nobody wrote
# promises the removal of something that is not deprecated, and a marker whose replacement names no
# function sends a migrating author to a door that was never mounted. This asks the package that
# deprecated something in this change — kit/rest — about its own two halves; the record's other rows
# were listed by other changes, and a case that holds the whole tree to this one's standard fails
# whenever any of them moves.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
pkg="$root/kit/rest"
record="$root/docs/compatibility/v3.0.0.md"
failures=0
say() { printf '%s %s\n' "$1" "$2"; }

[ -f "$record" ] || { echo "FAIL: $record is missing"; exit 1; }

# marked — each exported function in the package that carries a `// Deprecated:` comment above it.
marked="$( { grep -rn '^// Deprecated:' "$pkg"/*.go 2>/dev/null || true; } | grep -v '_test\.go' \
	| while IFS=: read -r file line _; do
		awk -v n="$line" 'NR > n && /^func / {
			s = $0; sub(/^func /, "", s)
			if (s ~ /^\(/) { sub(/^[^ ]+ [^ ]+\) /, "", s) }
			sub(/\[.*/, "", s); sub(/\(.*/, "", s)
			print s; exit
		}' "$file"
	done | sort -u)"
[ -n "$marked" ] || { echo "FAIL: kit/rest marks no deprecation, so this case has nothing to read"; exit 1; }

# listed — each identifier the record lists for this package. A package whose markers have no row has to
# reach the comparison below, so a grep that finds nothing is an empty list here, not a dead run.
listed="$( { grep -o '`kit/rest\.[A-Za-z]*`' "$record" || true; } | sed 's|`kit/rest\.||; s|`||' | sort -u)"

if [ "$marked" != "$listed" ]; then
	say FAIL "kit/rest marks$(printf ' %s' $marked) but the record lists$(printf ' %s' $listed)"
	failures=$((failures + 1))
else
	say ok "the record lists exactly the$(printf ' %s' $marked) that kit/rest marks deprecated"
fi

# Each marker's own sentence names the replacement; a migrating author who follows that sentence
# rather than the record has to land on a function the package publishes.
while IFS=: read -r file line _; do
	text="$(awk -v n="$line" 'NR == n' "$file")"
	word="$(printf '%s\n' "$text" | sed -n 's|^// Deprecated: [Uu]se \([A-Za-z0-9_.]*\).*|\1|p')"
	name="${word##*.}"
	if [ -z "$name" ]; then
		say FAIL "$(basename "$file"):$line names no replacement to use"
		failures=$((failures + 1))
		continue
	fi
	if grep -q "^func ${name}\[\|^func ${name}(" "$pkg"/*.go; then
		say ok "$(basename "$file"):$line sends the author to ${name}, which kit/rest publishes"
	else
		say FAIL "$(basename "$file"):$line sends the author to ${name}, which kit/rest does not publish"
		failures=$((failures + 1))
	fi
done < <(grep -rn '^// Deprecated:' "$pkg"/*.go | grep -v '_test\.go')

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
