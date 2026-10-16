#!/usr/bin/env bash
# The applied-migration check's cases, each in a throwaway repository: an edited (committed or only
# staged), deleted or renamed applied file is refused, a new file and a file put back to the applied bytes pass, the applied bytes
# are the oldest release's the base descends from when one carries the file, a quoted path and a line
# ending Git normalizes are judged by their own bytes, and a missing base or tag set is refused.
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/check_applied_migrations.sh"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
failures=0

commit() { git add -A && git -c user.name=t -c user.email=t@example.invalid commit -qm "$1"; }

fresh() { # a repository with two applied files and a v0.1.0 release; base is its HEAD
	local dir="$root/$1"
	mkdir -p "$dir/migrations" "$dir/modules/x/migrations" "$dir/modules/x/testdata" && cd "$dir" && git init -q -b main
	echo 'CREATE TABLE a ();' > migrations/000001_a.up.sql
	echo 'CREATE TABLE b ();' > modules/x/migrations/000002_b.up.sql
	echo 'SELECT 1;' > modules/x/testdata/fixture.sql
	echo 'The kernel schema.' > migrations/README.md
	commit 'base'
	git tag v0.1.0
	base="$(git rev-parse HEAD)"
}

expect() { # expect <exit code> <case name> <text the output must hold> [argument]
	local want="$1" name="$2" text="$3" got=0
	shift 3
	bash "$script" "$@" >"$root/out" 2>&1 </dev/null || got=$?
	if [ "$got" != "$want" ] || ! grep -qF -- "$text" "$root/out"; then
		echo "FAIL: $name: exit $got, wanted $want with \"$text\""
		sed 's/^/    /' "$root/out"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

fresh comment
echo '-- explain' >> migrations/000001_a.up.sql && commit 'fix: a comment in an applied file'
expect 1 'a comment edit to an applied file is refused' \
	'migrations/000001_a.up.sql changed after it was applied; add a new migration' "$base"

# A commit takes the index: an edit staged and then undone in the working tree still ships.
fresh staged
echo '-- explain' >> migrations/000001_a.up.sql && git add migrations/000001_a.up.sql
git show HEAD:migrations/000001_a.up.sql > migrations/000001_a.up.sql
expect 1 'an edit staged but not in the working tree is refused' \
	'migrations/000001_a.up.sql changed after it was applied; add a new migration' "$base"

fresh module
echo 'CREATE TABLE b (n int);' > modules/x/migrations/000002_b.up.sql && commit 'fix: a module file'
expect 1 'an edit inside a module migrations directory is refused' \
	'modules/x/migrations/000002_b.up.sql changed after it was applied' "$base"

fresh added
echo 'CREATE TABLE c ();' > migrations/000003_c.up.sql && commit 'feat: a new migration'
expect 0 'a new migration passes' 'no applied migration changed' "$base"

fresh removed
git rm -q migrations/000001_a.up.sql && commit 'chore: drop an applied file'
expect 1 'a deleted applied file is refused' \
	'migrations/000001_a.up.sql was applied but is missing from this tree' "$base"

fresh renamed
git mv migrations/000001_a.up.sql migrations/000004_a.up.sql && commit 'chore: renumber'
expect 1 'a renamed applied file is refused under its old path' \
	'migrations/000001_a.up.sql was applied but is missing from this tree' "$base"

# The base carries an edit to a file no release has: installations hold the first-parent add's bytes.
fresh restore
echo 'CREATE TABLE d ();' > migrations/000005_d.up.sql && commit 'feat: d'
echo '-- renamed test' >> migrations/000005_d.up.sql && commit 'fix: edit d after it shipped'
base="$(git rev-parse HEAD)"
echo 'CREATE TABLE d ();' > migrations/000005_d.up.sql && commit 'fix: d carries its applied bytes'
expect 0 'a file put back to its first-parent bytes passes' \
	'migrations/000005_d.up.sql restores the bytes installations applied' "$base"

# A file edited before its first release: the release's bytes are what installations hold.
fresh released
echo 'CREATE TABLE e ();' > migrations/000006_e.up.sql && commit 'feat: e'
echo 'CREATE TABLE e (n int);' > migrations/000006_e.up.sql && commit 'fix: e before release'
git tag v0.2.0
base="$(git rev-parse HEAD)"
echo 'CREATE TABLE e ();' > migrations/000006_e.up.sql && commit 'fix: e back to its first bytes'
expect 1 'a file put back to pre-release bytes is refused' \
	'migrations/000006_e.up.sql changed after it was applied' "$base"

fresh proof
echo 'CREATE TABLE f ();' > migrations/000007_f.up.sql && commit 'feat: f'
echo 'CREATE TABLE f (n int);' > migrations/000007_f.up.sql && commit 'fix: f on a proof'
git tag v0.2.0-proof.1
echo 'CREATE TABLE f (m int);' > migrations/000007_f.up.sql && commit 'fix: f again'
base="$(git rev-parse HEAD)"
echo 'CREATE TABLE f (n int);' > migrations/000007_f.up.sql && commit 'fix: f to the proof bytes'
expect 1 'a proof tag is not a release' 'migrations/000007_f.up.sql changed after it was applied' "$base"

# A release cut on the candidate is not history the base's installations ran.
fresh unreleased
echo 'CREATE TABLE h ();' > migrations/000009_h.up.sql && commit 'feat: h'
base="$(git rev-parse HEAD)"
echo '-- explain' >> migrations/000009_h.up.sql && commit 'fix: h after it shipped'
git tag v0.2.0
expect 1 'a release tag the base does not descend from is not the applied bytes' \
	'migrations/000009_h.up.sql changed after it was applied' "$base"

fresh quoted
git config core.quotePath true
echo 'CREATE TABLE ç ();' > migrations/000010_ç.up.sql && commit 'feat: ç'
base="$(git rev-parse HEAD)"
echo '-- explain' >> migrations/000010_ç.up.sql && commit 'fix: ç after it shipped'
expect 1 'a path Git quotes is judged under its own name' \
	'migrations/000010_ç.up.sql changed after it was applied' "$base"

# go:embed ships the bytes on disk, so a line ending autocrlf hides from Git is still an edit.
fresh crlf
git config core.autocrlf true
printf 'CREATE TABLE a ();\r\n' > migrations/000001_a.up.sql
expect 1 'a line ending Git normalizes is a changed byte' \
	'migrations/000001_a.up.sql changed after it was applied' "$base"

fresh outside
echo 'SELECT 2;' > modules/x/testdata/fixture.sql
echo 'Applied bytes never change.' > migrations/README.md && commit 'docs: outside the judged files'
expect 0 'testdata SQL and a README beside migrations are not judged' 'no applied migration changed' "$base"

fresh same
expect 0 'a base equal to HEAD says it compared nothing' 'base is HEAD; nothing to compare' "$base"

fresh malformed
expect 2 'a short base is refused' 'supply a nonzero full base commit SHA' abc
expect 2 'an all-zero base is refused' 'supply a nonzero full base commit SHA' 0000000000000000000000000000000000000000
expect 2 'an unknown base with no origin is refused' 'could not fetch base commit' \
	1234567890123456789012345678901234567890

fresh untagged
git tag -d v0.1.0 >/dev/null
echo '-- explain' >> migrations/000001_a.up.sql && commit 'fix: a comment'
expect 2 'a repository with no release tag is refused' 'no release tag in this repository' "$base"

# With no argument the base is the merge base with origin/main, never a stale local main.
fresh remote
upstream="$root/remote-upstream"
git clone -q --bare . "$upstream"
git remote add origin "$upstream"
git branch -q stale "$(git rev-parse HEAD)"
echo 'CREATE TABLE g ();' > migrations/000008_g.up.sql && commit 'feat: g'
git push -q origin HEAD:main 2>/dev/null
git fetch -q origin
git checkout -q -b topic
echo '-- explain' >> migrations/000008_g.up.sql && commit 'fix: g after it merged'
git branch -q -f main stale
expect 1 'without an argument the base is origin/main, not local main' \
	'migrations/000008_g.up.sql changed after it was applied'

fresh nobase
git tag -d v0.1.0 >/dev/null
expect 2 'no argument, no origin/main and no release tag is refused' 'no base'

if [ "$failures" -ne 0 ]; then
	echo "$failures case(s) failed"
	exit 1
fi
echo 'check-applied-migrations: every case answered'
