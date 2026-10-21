#!/usr/bin/env bash
# Every script test that merges in a throwaway repository sets a repository-local
# committer, because a merge takes its identity from the repository and a CI
# container names none.
set -euo pipefail
cd "$(dirname "$0")/.."
bad=0
for f in scripts/*_test.sh; do
	grep -q 'git merge' "$f" && grep -q 'git init' "$f" || continue
	grep -q 'config --local user.email' "$f" || { echo "FAIL $f merges without a repository-local identity"; bad=1; }
done
[ "$bad" = 0 ] && echo 'ok   every merging fixture names its committer'
exit "$bad"
