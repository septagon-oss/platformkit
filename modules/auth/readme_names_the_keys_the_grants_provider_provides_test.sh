#!/usr/bin/env bash
# The Composition paragraph of this module's README states which keys the
# `authgrants` provider publishes and which files hold the decision and the
# capabilities. Those are claims about Go identifiers, and nothing reads
# documentation as Go: a paragraph naming `grants.Authorizer` while the provider
# declares `contracts.GrantAuthorizer` compiles, serves and ships, and a reader
# who follows the link or copies the name finds neither. So this asks the two
# questions only documentation gets wrong and the build cannot notice — that
# every published key is one the provider declares, and that every path the
# module links to is a file that exists.
#
# Run: bash modules/auth/readme_names_the_keys_the_grants_provider_provides_test.sh
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")"
readme=README.md
provider=grants.go
fails=''

fail() { fails="${fails:+$fails$'\n'}$1"; }

[ -f "$readme" ] || { echo "no README at $readme" >&2; exit 2; }
[ -f "$provider" ] || { echo "no grant provider at $provider" >&2; exit 2; }

# The keys the paragraph publishes, in its own words: the backticked names
# between "It publishes" and the semicolon that closes that sentence.
keys="$(tr '\n' ' ' < "$readme" | awk '{ i = index($0, "It publishes ") } i { s = substr($0, i + 13); j = index(s, ";"); print (j ? substr(s, 1, j - 1) : s) }' \
	| grep -o '`[^`]*`' | tr -d '`' | tr -s '[:space:]' '\n' | grep .)" || keys=''
[ -n "$keys" ] || fail "$readme: no 'It publishes …' sentence to ask anything of"

for key in $keys; do
	# Each published key appears in the provider as a typed declaration of its
	# own, spelled with the same dotted name the README uses. A key that moved,
	# was renamed or was never declared falls out of this list.
	grep -qF "pkit.Provides[$key]()" "$provider" \
		|| fail "$readme: publishes \`$key\`, which $provider does not declare as pkit.Provides[$key]()"
done

# Every path a reader can follow out of this README — inside the module or up
# into the repository — has to be there. An anchor is checked as its file.
while IFS= read -r link; do
	[ -n "$link" ] || continue
	case "$link" in http://*|https://*|*://*) continue ;; esac
	[ -e "${link%%#*}" ] || fail "$readme: links $link, which is not a file of this repository"
done < <(grep -o ']([^)]*)' "$readme" | sed 's/^](//; s/)$//' | grep -v '^#' | sort -u)

if [ -n "$fails" ]; then
	printf '%s\n' "$fails" >&2
	echo 'readme: the grant provider paragraph and the provider disagree' >&2
	exit 1
fi
echo "readme: $(printf '%s\n' "$keys" | wc -l) published keys each declared in $provider; every linked path exists"
