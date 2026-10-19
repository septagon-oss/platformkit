#!/usr/bin/env bash
# An upstream migration added after a topic branched is not a deletion by that topic.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_applied_migrations.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
cd "$root"

commit() {
	git add -A
	git -c user.name=fixture -c user.email=fixture@example.invalid commit -qm "$1"
}

git init -q -b main
mkdir migrations
echo 'CREATE TABLE a (id int);' > migrations/000001_a.up.sql
commit 'initial schema'
git tag v0.1.0
git checkout -q -b topic
echo 'Application documentation.' > README.md
commit 'documentation'
git checkout -q main
echo 'CREATE TABLE b (id int);' > migrations/000002_b.up.sql
commit 'new upstream migration'
base="$(git rev-parse HEAD)"
git checkout -q topic

# The same topic must pass with both the inferred remote base and CI's explicit base SHA.
git update-ref refs/remotes/origin/main "$base"
bash "$script" > "$root/inferred.out" 2>&1 </dev/null
got=0
bash "$script" "$base" > "$root/explicit.out" 2>&1 </dev/null || got=$?
if [ "$got" -ne 0 ]; then
	echo "FAIL: a documentation-only topic must pass against its merge base; explicit base exited $got"
	cat "$root/explicit.out"
	exit 1
fi
echo 'ok: inferred and explicit bases both judge only the topic change'
