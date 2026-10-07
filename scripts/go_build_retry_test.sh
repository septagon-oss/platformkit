#!/usr/bin/env bash
# Which failure of a `go build` says something about the tree, and which says something about the
# machine's shared build cache — and what the retry does when the cache never heals.
#
# `make check` died in check-rehearse at 08:53Z on 2026-10-07 with a build of the previous release that
# reported archives it had *written* as missing —
#
#	modules/task/contracts/task.go:13:2: could not import context (open
#	/home/jplr/.cache/go-build/08/088b62…-d: no such file or directory)
#	modules/tenant/internal/handler.go:170:95: undefined: huma
#
# — and named seven packages in one pass, each in whichever line first read the archive that had gone.
# `go` closes every command by trimming the one cache directory every worktree on this host shares
# (cache.go: `Close()` is `Trim()`), removing entries whose "last used" mtime is older than `trimLimit`
# and refreshing that mtime at most once an hour, so a build that opens an entry twice — which is what
# loading a compiled archive is — can have it removed in between, by another task's `go`. The same
# command on the same exported tree exited 0 in 5.0s minutes later.
#
# So the two ways to get this wrong are the two this file pins: a step that ends the whole gate over a
# file the machine pulled out from under it (what the branch had), and a step that retries whatever the
# build complained about, which would make "the previous release does not build" — a fact a release
# needs — unreportable. The cases below answer `go` itself, with the exact sentence from the gate log,
# so nothing here needs a second tree, a compiler or a network.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
script="$root/scripts/go_build_retry.sh"
failures=0
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The build's own words, from gate-implement.log at 08:53Z on 2026-10-07: the two packages the gate
# named first, in the order the gate printed them.
CACHE_RACE='modules/task/contracts/task.go:13:2: could not import context (open /home/jplr/.cache/go-build/08/088b6284723aab346bd7c558014ee461e8fa550588a4fd6c58e27888d23bb8c7-d: no such file or directory)
modules/tenant/internal/handler.go:170:95: undefined: huma'

# A source file that is not there says so in the same tense. That is a fact about the checkout.
SOURCE_MISSING='modules/task/contracts/task.go:13:2: could not import context (open /src/modules/task/contracts/task.go: no such file or directory)'
COMPILE_ERROR='modules/auth/topaz.go:16:28: undefined: authorizer'

verdict() { # verdict <tries spent> <output the build wrote> — "wait <seconds>" when another try is owed, "final" otherwise
	local out
	out="$(bash -c '
		set -euo pipefail
		# shellcheck source=scripts/go_build_retry.sh
		. "$1/scripts/go_build_retry.sh"
		if w="$(build_retry_wait "$2" "$3")"; then printf "wait %s" "$w"; else printf "final"; fi
	' bash "$root" "$1" "$2")" || out="final"
	printf '%s' "$out"
}

expect() { # expect <want> <case name> <tries spent> <output the build wrote>
	local want="$1" name="$2" got
	got="$(verdict "$3" "$4")"
	if [ "$got" != "$want" ]; then
		echo "FAIL: $name: got '$got', wanted '$want'"
		failures=$((failures + 1))
	else
		echo "ok   $name"
	fi
}

# 1. The observed failure is retried, and the wait before the next try is the first one.
expect "wait 5" "a build whose cache entry vanished is retried" 0 "$CACHE_RACE"

# 2. A cache moved off the default path is still the machine's fault: the sentence names the directory
#    `go env GOCACHE` gives, and reading that is the other half of the answer.
out_of_the_way="$work/moved-cache"
mkdir -p "$out_of_the_way"
if moved="$(GOCACHE="$out_of_the_way" bash -c '
	set -euo pipefail
	# shellcheck source=scripts/go_build_retry.sh
	. "$1/scripts/go_build_retry.sh"
	build_cache_race "kit/rest/serve.go:4:2: could not import net/http (open $2/1a/1a7c-d: no such file or directory)"
' bash "$root" "$out_of_the_way")"; then
	echo "ok   a cache entry named under GOCACHE, wherever it lives, is read as the machine's"
else
	echo "FAIL: a cache entry under \$GOCACHE=$out_of_the_way was not read as the machine's (exit $?)"
	failures=$((failures + 1))
fi

# 3. What a checkout writes is never retried.
expect "final" "a compile error is reported on the first try" 0 "$COMPILE_ERROR"
expect "final" "a source file that is not there is reported on the first try" 0 "$SOURCE_MISSING"

# 4. The retry is bounded: two waits, then the build's own failure is what the step reports.
expect "wait 20" "the second blip waits the longer wait" 1 "$CACHE_RACE"
expect "final" "the third blip is the failure the run reports" 2 "$CACHE_RACE"

# A `go` that answers from a script, so the helper's own loop runs end to end: one line of WAYS per
# call it is asked to build, `ok <text>` for a build that wrote text and exited 0, `<code> <text>` for
# one that wrote text and exited that code. `go env GOCACHE` is answered without consuming a line.
mkdir -p "$work/bin"
printf '%s\n' 0 >"$work/calls"
cat >"$work/bin/go" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" = env ] && [ "$2" = GOCACHE ]; then
	printf '%s\n' "${FAKE_GO_CACHE_DIR:-/home/build/.cache/go-build}"
	exit 0
fi
n=$(( $(cat "$FAKE_GO_CALLS") + 1 ))
printf '%s\n' "$n" >"$FAKE_GO_CALLS"
way="$(sed -n "${n}p" "$FAKE_GO_WAYS")"
if [ -z "$way" ]; then
	printf 'no scripted answer for call %s\n' "$n" >&2
	exit 9
fi
code="${way%% *}"; text="${way#* }"
if [ "$code" = "$way" ]; then text=""; fi
if [ "$code" = "ok" ]; then
	if [ -n "$text" ]; then printf '%s\n' "$text"; fi
	exit 0
fi
if [ -n "$text" ]; then printf '%s\n' "$text" >&2; fi
exit "$code"
FAKE
chmod +x "$work/bin/go"

# driven <case name> <want exit> <want calls> <scripted answers> <needles…>
# Runs the helper's own loop against the scripted `go`, then judges what the caller saw: the exit code,
# how many builds ran, and the lines the run left for a reader.
driven() {
	local name="$1" want_code="$2" want_calls="$3" ways="$4"
	shift 4
	local out got calls
	: >"$work/calls"
	printf '%s\n' "$ways" >"$work/ways"
	out="$(PATH="$work/bin:$PATH" FAKE_GO_CALLS="$work/calls" FAKE_GO_WAYS="$work/ways" bash -c '
		set -euo pipefail
		# shellcheck source=scripts/go_build_retry.sh
		. "$1/scripts/go_build_retry.sh"
		shift
		go_build_retry "$@"
	' bash "$root" go build -o /dev/null ./apps/platformkit 2>&1)" && got=0 || got=$?
	calls="$(cat "$work/calls")"
	if DRIVEN_OUT="$out" DRIVEN_GOT="$got" DRIVEN_CALLS="$calls" DRIVEN_NAME="$name" \
		python3 - "$want_code" "$want_calls" "$@" <<'PY'
import os
import sys

want_code, want_calls = int(sys.argv[1]), int(sys.argv[2])
out, name = os.environ["DRIVEN_OUT"], os.environ["DRIVEN_NAME"]
got, calls = int(os.environ["DRIVEN_GOT"]), int(os.environ["DRIVEN_CALLS"])
problems = []
if got != want_code:
    problems.append(f"exit {got}, wanted {want_code}")
if calls != want_calls:
    problems.append(f"{calls} build(s) ran, wanted {want_calls}")
for needle in sys.argv[3:]:
    if needle not in out:
        problems.append(f"no {needle!r} in what the caller saw")
if problems:
    print(f"FAIL: {name}: " + "; ".join(problems))
    for line in out.splitlines()[-8:]:
        print(f"    {line}")
    sys.exit(1)
print(f"ok   {name}")
PY
	then :; else failures=$((failures + 1)); fi
}

# 5. End to end: the first try loses the archive, the second builds, and what the step sees is a build
#    that succeeded — on its second try, said out loud, because a reader weighs the result later.
driven "a build that lost its cache entry is built on the second try" 0 2 \
	'1 modules/task/contracts/task.go:13:2: could not import context (open /home/build/.cache/go-build/08/088b62-d: no such file or directory)
ok' "try 2" "built on try 2" || :

# 6. A compile error is forwarded whole, once, with the command's own exit code: nothing about the
#    retry may stand between a reviewer and the sentence they need.
driven "a build that fails over its source is refused on the first try" 1 1 \
	'1 modules/auth/topaz.go:16:28: undefined: authorizer' "undefined: authorizer" || :

# 7. A cache that never heals is still refused, after both waits and three tries, and the report names
#    the entry the last try could not read. 25 s is the bound itself rather than a number asserted
#    about it, paid here the way the API gate's case pays its own.
driven "a build whose cache never heals is refused after both waits" 1 3 \
	'1 could not import context (open /home/build/.cache/go-build/08/088b62-d: no such file or directory)
1 could not import context (open /home/build/.cache/go-build/09/09f11b-d: no such file or directory)
1 could not import context (open /home/build/.cache/go-build/0a/0af11b-d: no such file or directory)' \
	"try 2" "try 3" "0af11b" || :

# 8. The step that asked for the helper still asks it, both times, and no build is left bare: a cure
#    edited out of the one caller keeps every case above green.
rehearsal="$root/scripts/rehearse_migrations.sh"
through="$(grep -c 'go_build_retry go build' "$rehearsal")"
bare="$(grep -n 'go build' "$rehearsal" | grep -v '^[0-9][0-9]*:[[:space:]]*#' |
	grep -vc 'go_build_retry go build' || true)"
sourced="$(grep -c '^\.[[:space:]]*"\$root/scripts/go_build_retry.sh"' "$rehearsal")"
if [ "$through" -ne 2 ] || [ "$bare" -ne 0 ] || [ "$sourced" -ne 1 ]; then
	echo "FAIL: rehearse_migrations.sh: $through build(s) through the retry, $bare bare, $sourced source line(s); wanted 2, 0, 1"
	failures=$((failures + 1))
else
	echo "ok   the rehearsal's two builds both go through the retry, and none is left bare"
fi

if [ "$failures" -ne 0 ]; then
	echo "go_build_retry: $failures case(s) failed" >&2
	exit 1
fi
echo "go_build_retry: a lost build-cache entry is rebuilt after a wait and refused after two; a build that fails over its source is refused at once, with its own exit code"
