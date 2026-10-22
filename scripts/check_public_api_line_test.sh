#!/usr/bin/env bash
# What a frozen release line refuses, and what it does not see.
#
# `make check-apidiff` compares the tree against the one revision the line record names
# and passes no list of accepted changes, so every reported incompatibility is a refusal.
# The claim has two halves, and neither is answerable from this repository's own surface:
# a throwaway removal has to actually be refused and a throwaway addition actually
# accepted. So these cases run the shipped gate, unmodified, over scratch modules built
# under mktemp -d — the manner of scripts/e2e_step_library_test.sh — where a `git init`, a
# go.mod naming `example.org/library` and one package of Go stand for a published surface.
#
# Why a scratch tree rather than the existing fake: scripts/check_public_api_retry_test.py
# answers what the gate does with a report it is handed, which is the right tool for that
# question. What only a real comparison answers is what apidiff itself reports — that a
# method added to an exported interface is an incompatibility rather than an addition, and
# that a name absent at the compared revision is reported as `added` with no word about its
# shape. Both are properties of the tool, and the whole value of freezing a line rests on
# them, so they are measured here rather than argued.
#
# Every case is one run of the shipped script: its exit code, the evidence JSON on stdout
# and the lines on stderr. Nothing here re-implements the verdict, so no case can pass over
# a gate that stopped refusing. Like the target, these runs need the module proxy.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bash "$root/scripts/check_public_api_frozen_commit_test.sh"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT
failures=0
src="$temporary/library"
# The line record lives outside the scratch work tree: a case checks out the commit before
# the record was written, and a record that vanishes with that checkout would be a case
# that refuses for the wrong reason.
records="$temporary/records"
record="$records/line.json"
status=0

# The scratch surface, in pieces: an exported interface, function and struct — enough that
# a removal, an addition, an interface widening and a reshape are four different reports.
allow=$'\tAllow(key string) bool\n'
reserve=$'\tReserve(key string, n int) bool\n'
reserve_unit=$'\tReserve(key string, n int, unit string) bool\n'
forget=$'\tForget(key string)\n'
store_open=$'// Open returns the store.\nfunc Open(path string) (*Store, error) { return nil, nil }\n\n'
store=$'// Store holds things.\ntype Store struct{}\n'
probe=$'// Probe is new and exported.\nfunc Probe() int { return 1 }\n\n'

surface() { # surface <interface body> <declarations> — writes library/library.go
	{
		printf 'package library\n\n// Limiter answers whether an action may happen.\ntype Limiter interface {\n'
		printf '%s' "$1"
		printf '}\n\n'
		printf '%s' "$2"
	} >"$src/library/library.go"
}

commit_surface() { # commit_surface <message> <interface body> <declarations>
	surface "$2" "$3"
	git add -A
	git commit -qm "$1"
}

freeze_surface() { git rev-parse --verify --end-of-options "$1^{commit}"; }

run() { # run <arguments…> — one run of the shipped gate, exit code left in $status
	set +e
	(cd "$src" && timeout 600 python3 scripts/check_public_api.py "$@" \
		>"$temporary/out.json" 2>"$temporary/err")
	status=$?
	set -e
}

reported() { # reported — the incompatibility lines the last run reported, one per line
	python3 - "$temporary/out.json" <<'PY'
import json, sys
try:
    report = json.load(open(sys.argv[1])).get("incompatibilities", "")
except (FileNotFoundError, json.JSONDecodeError):
    report = ""
print("\n".join(line[2:] for line in report.splitlines() if line))
PY
}

field() { # field <dotted key> — one field of the last run's evidence JSON
	python3 - "$temporary/out.json" "$1" <<'PY'
import json, sys
value = json.load(open(sys.argv[1]))
for part in sys.argv[2].split("."):
    value = value[part]
print(str(value).lower() if isinstance(value, bool) else value)
PY
}

refuse() { # refuse <case name> <wanted exit code> — the verdict `make check-apidiff` returns
	if [ "$status" != "$2" ]; then
		echo "FAIL: $1: exit $status, wanted $2"
		printf '    %s\n' "$(tail -6 "$temporary/err")"
		failures=$((failures + 1))
		return 1
	fi
	return 0
}

reports() { # reports <case name> <fixed string> — a run must have reported that sentence
	local lines
	lines="$(reported)"
	case "$lines" in
		*"$2"*) return 0 ;;
	esac
	echo "FAIL: $1: reported no line holding '$2'"
	printf '    reported: %s\n' "$(printf '%s' "$lines" | tr '\n' ';')"
	failures=$((failures + 1))
}

says() { # says <case name> <fixed string> — a run must have written that line on stderr
	if ! grep -qF -- "$2" "$temporary/err"; then
		echo "FAIL: $1: stderr holds no '$2'"
		printf '    %s\n' "$(tail -6 "$temporary/err")"
		failures=$((failures + 1))
	fi
}

holds() { # holds <case name> <evidence field> <wanted> — what the evidence says about the base
	local got
	got="$(field "$2" 2>/dev/null || echo unreadable)"
	if [ "$got" != "$3" ]; then
		echo "FAIL: $1: evidence $2 is '$got', wanted '$3'"
		failures=$((failures + 1))
	fi
}

ok() { # ok <case name> — one line per case, and only while nothing has refused
	if [ "$failures" -eq 0 ]; then echo "ok   $1"; fi
}

# ---------------------------------------------------------------- the scratch line

mkdir -p "$src/library" "$src/scripts/baselines" "$records"
cp "$root/scripts/check_public_api.py" "$src/scripts/"
printf 'module example.org/library\n\ngo 1.26.6\n' >"$src/go.mod"
cd "$src"
git init -q .
git config user.email "worker@platformkit.test"
git config user.name "Scratch Fixture"
commit_surface "the surface a consumer compiled against" "$allow" "$store_open$store"
git tag v1.0.0
commit_surface "the line accepts Limiter.Reserve" "$allow$reserve" "$store_open$store"
freeze="$(freeze_surface HEAD)"
python3 - "$record" "$freeze" <<'PY'
import json, sys
json.dump({"note": "the scratch line's record", "line": "v2.0.0",
           "frozen": {"commit": sys.argv[2]}}, open(sys.argv[1], "w"), indent=2)
PY

# The line's head, as a branch off the freeze: every case below is a branch of it, so one
# case's tree cannot leak into another's verdict.
head_surface() { # head_surface <name> <interface body> <declarations>
	git checkout -q "$freeze"
	git switch -qc "$1"
	commit_surface "$1" "$2" "$3"
}

# 1. A removal is refused, and a file naming that removal changes nothing. The gate passes
#    no list, which is the only formulation left once a listing is shown to license what it
#    names: a baseline that carries a removal turns it into a pass.
head_surface removed-exported-name "$allow$reserve" "$store"
python3 - "$src/scripts/baselines/listed-removal.json" "$freeze" <<'PY'
import json, sys
json.dump({"note": "a list naming the removal beside it", "line": "v2.0.0",
           "frozen": {"commit": sys.argv[2]},
           "incompatibilities": ["./library.Open: removed"]},
          open(sys.argv[1], "w"), indent=2)
PY
git add -A
git commit -qm "a file records the removal"
run --line-record "$record" HEAD
if refuse "refuses a removed exported name" 1; then
	reports "refuses a removed exported name" "./library.Open: removed"
	says "refuses a removed exported name" "BREAK ./library.Open: removed"
fi
ok "refuses a removed exported name"

# 2. The same tree, with the list it was written for beside it. The flag is gone, so what a
#    contributor used to reach for to pass a break is refused outright.
run --line-record "$record" --baseline scripts/baselines/listed-removal.json HEAD
if refuse "refuses a removal somebody recorded" 2; then
	says "refuses a removal somebody recorded" "unrecognized"
fi
ok "refuses a removal somebody recorded, because the gate takes no list"

# 3. A compatible addition is not a break: the line only grows.
head_surface compatible-addition "$allow$reserve" "$probe$store_open$store"
run --line-record "$record" HEAD
if refuse "accepts a throwaway compatible addition" 0; then
	holds "accepts a throwaway compatible addition" result \
		"no exported incompatibility with the frozen line"
	holds "accepts a throwaway compatible addition" incompatibilities ""
fi
ok "accepts a throwaway compatible addition"

# 4. An exported interface is not a surface that only grows: a method added to one breaks
#    every implementation a consumer wrote, and the tool says so. Most of the changes the
#    retired list carried were this shape, which is why the rule names it.
head_surface method-added-to-an-exported-interface "$allow$reserve$forget" "$store_open$store"
run --line-record "$record" HEAD
if refuse "refuses a method added to an exported interface" 1; then
	reports "refuses a method added to an exported interface" "./library.Limiter.Forget: added"
fi
ok "refuses a method added to an exported interface"

# 5. The hole the freeze closes. v1.0.0 never saw Reserve, so the comparison describes it
#    as `added` and says nothing of its shape: a base older than an addition cannot refuse
#    a reshaping of it. The freeze commit carries Reserve, so the same candidate comes back
#    `changed from` — the difference between a base tag and a frozen surface.
head_surface reshaped-after-the-tag "$allow$reserve_unit" "$store_open$store"
run --line-record "$record" HEAD
if refuse "refuses a reshape of a name added after the base tag" 1; then
	reports "refuses a reshape of a name added after the base tag" \
		"./library.Limiter.Reserve: changed from func(string, int) bool"
fi
run v1.0.0 HEAD
if refuse "measures the same reshape against the older tag" 1; then
	reports "measures the same reshape against the older tag" \
		"./library.Limiter.Reserve: added"
fi
ok "refuses a reshape of a name added after the base tag, which an older base cannot see"

# 6. A branch cut before the freeze would be refused for the change the freeze itself
#    carries, so the gate judges a candidate from its merge base and says so. Main is
#    unaffected: the freeze is main's merge base, and there the run judges the frozen
#    surface. The pair below is both halves of that rule on one tree.
git checkout -q v1.0.0
git switch -qc branch-before-the-freeze
commit_surface "an addition on a branch older than the freeze" "$allow" "$probe$store_open$store"
run --line-record "$record" HEAD
if refuse "judges a branch that precedes the freeze by its own delta" 0; then
	holds "judges a branch that precedes the freeze by its own delta" line.judged \
		"this change's own delta"
fi
run "$freeze" HEAD
if refuse "refuses the same branch when the freeze is named as its base" 1; then
	reports "refuses the same branch when the freeze is named as its base" \
		"./library.Limiter.Reserve: removed"
fi
ok "judges a branch that precedes the freeze by its own delta"

# 7. Once the owner publishes the line's tag, the record's `line` names it and the resolver
#    prefers it to the freeze: the switch is a tag appearing, not a later commit that could
#    name the wrong base before the tag exists. The evidence says which base it used.
git tag -a v2.0.0 "$freeze" -m "the scratch line"
run --line-record "$record" "$freeze"
if refuse "prefers the line's tag once it exists" 0; then
	holds "prefers the line's tag once it exists" line.tag_exists true
	holds "prefers the line's tag once it exists" line.wanted "$(freeze_surface v2.0.0)"
fi
ok "prefers the line's tag once it exists"

# 8. A line with no base is a gate with no question, so the record is refused by name
#    before any tree is exported.
python3 - "$records/without-a-base.json" <<'PY'
import json, sys
json.dump({"note": "a record whose tag was never published", "line": "v9.9.9"}, open(sys.argv[1], "w"))
PY
run --line-record "$records/without-a-base.json" HEAD
if refuse "refuses a line record that names no base" 2; then
	says "refuses a line record that names no base" \
		"without-a-base.json names neither a published tag nor a frozen commit"
fi
ok "refuses a line record that names no base"

if [ "$failures" -ne 0 ]; then
	echo "check_public_api line: $failures case(s) failed" >&2
	exit 1
fi
echo "check_public_api line: a frozen surface refuses every reported break and accepts a compatible addition"
