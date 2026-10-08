#!/usr/bin/env bash
# Which Go cache each CI job restores, and what may never be true about it.
#
# Both Go jobs of .gitea/workflows/ci.yml restored nothing until T-0277: `cache: false` on their
# `actions/setup-go` steps meant every run re-fetched every module in go.sum and recompiled every
# package, measured on this tree at 28.1s of wall over 228.3s of CPU into an empty GOCACHE against
# 2.9s over 4.7s into a warm one, for `go build ./...` alone at the job's own `-p=4`. The cure is
# four steps per job — name the key, restore, run, save — and a cure that lives in YAML rots in
# YAML: the two things this change promises are exactly the two a later edit can quietly undo from
# a different file.
#
# The first promise is the key's parts. A key that does not change when go.sum changes hands every
# commit the same stale archive forever; a key computed with `hashFiles` on this runner is worse than
# stale, because act_runner runs hashFiles as a node process inside the job container and returns
# `""` with no error when it gets nothing back (act/runner/expression.go, act_runner v3.3.2), which
# freezes the key across commits and makes every later run take an exact hit and never save. The
# digest therefore lives in scripts/ci_go_cache_key.sh, which both Go jobs run — one recipe, one key
# per job, since a key both of them saved under would belong to whichever archive the store kept — and
# cases 5 to 7 below are about that file: read, checked for the refusal, then executed and asked what
# it actually answers. Case 1 refuses a cache step that reaches for hashFiles anyway.
#
# The second promise is the brief's: a cache miss is never a failure, and no test result survives a
# commit. The first is four properties of the steps (cases 2, 4 and 5) and the second is not in this
# file at all — since T-0310 it is the default of one Makefile variable that the two suite goals read,
# which is precisely why case 3 reads the Makefile: a build cache restored across commits cannot
# resurrect a test result while that default and those two interpolations stand, and it can the moment
# someone "optimises" the count into GOFLAGS.
#
# The cases read the workflow, the Makefile and that one script, and start no server; case 7 runs the
# script for real in a temporary directory, which costs one `go env` and three `sha256sum`s.
#
# Case 11 is here because CI refused the head `5bf518b2` for a tool the tree cannot see: `make check`
# died at Makefile line 322 with exit 1 and no assertion, which is what `import yaml` answers in a job
# image that ships python3 and no PyYAML. A guard that cannot parse the workflow it is reading is not a
# red check, it is no check at all, so the requirement is pinned below — asked of the Makefile, so the
# case names whichever check lines parse YAML tomorrow rather than the three that do today.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
ci="$root/.gitea/workflows/ci.yml"
makefile="$root/Makefile"
keyscript="$root/scripts/ci_go_cache_key.sh"
failures=0

fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# job_text JOB — the lines of that job's block. A workflow's jobs are the two-space keys under
# `jobs:`, and the block runs to the next two-space key that is not a comment.
job_text() {
	awk -v job="$1" '
		$0 ~ "^  " job ":[[:space:]]*$" { inside = 1; next }
		inside && $0 ~ "^  [^[:space:]#]" { inside = 0 }
		inside { print }
	' "$ci"
}

# The key recipe, read once: both jobs call this file, so the cases about the key read the file and
# not two copies of a `run:` block.
script="$(cat "$keyscript" 2>/dev/null || true)"
# Code lines only, for the two checks below about what the script can exit: its own header says the
# word exit while explaining why it must not.
script_code="$(grep -v '^[[:space:]]*#' <<<"$script")"
if [ -z "$script" ]; then
	echo "FAIL: $keyscript is missing: both Go jobs call it to form their key"
	exit 1
fi

# step_block JOB NAME — the whole step of that name, from its `- name:` line to the next step.
step_block() {
	job_text "$1" | awk -v name="$2" '
		$0 ~ /^[[:space:]]*- / { keep = (index($0, "- name: " name) == 7) }
		keep { print }
	'
}

# uses_block JOB REGEX — the step whose header line matches REGEX, to the next step, comment lines
# left out: the reason beside `cache: false` in this file runs to thirteen lines.
uses_block() {
	job_text "$1" | awk -v re="$2" '
		$0 ~ /^[[:space:]]*- / { keep = ($0 ~ re) }
		keep && $0 !~ /^[[:space:]]*#/ { print }
	'
}

# step_paths JOB NAME — the `path:` block of that step, one entry per line, leading and trailing
# whitespace off. @actions/cache hashes the path list into the archive's `version` alongside the key,
# so a save whose list differs from its restore's in ORDER alone writes an archive no restore of the
# same job can ever match — permanently cold, permanently green. Compared as an ordered list, and read
# only as far as the block is indented, because the next step's comment lines sit at the same column.
# Neither reader below `exit`s once it has its block: the writer beside it is still printing, and under
# `set -euo pipefail` the SIGPIPE it takes back is exit 141 — a red `make check` with no assertion in
# it, which is the class of red this branch was already called to look at. Stopping the collection is
# the same answer, asked of the one process that has it. Where the pipe fills is a scheduling question,
# and this file's own subject decides it: `job_text check` answers 36,779 bytes on this tree and 38,520
# on the tree of the commit after this one, and this file before the cure below answered twenty passes
# into the first and twenty exit-141 deaths into the second. A verdict that turns on how much prose a
# guard's subject carries dies on someone else's pull request.
step_paths() {
	step_block "$1" "$2" | awk '
		{ line = $0; sub(/[[:space:]]+$/, "", line) }
		line ~ /^[[:space:]]*path:[[:space:]]*[|]?[[:space:]]*$/ && !seen && !stopped {
			seen = 1; base = match(line, /[^[:space:]]/); next
		}
		seen && !stopped {
			if (line == "") next
			if (match(line, /[^[:space:]]/) <= base) { stopped = 1; next }
			sub(/^[[:space:]]+/, "", line)
			print line
		}
	'
}

# step_restore_keys JOB NAME — the `restore-keys:` block of that step, in the order written, which is
# the order the store is asked in. Reads to the end of the block for the reason written above.
step_restore_keys() {
	step_block "$1" "$2" | awk '
		{ line = $0; sub(/[[:space:]]+$/, "", line) }
		line ~ /^[[:space:]]*restore-keys:[[:space:]]*[|]?[[:space:]]*$/ && !seen && !stopped {
			seen = 1; base = match(line, /[^[:space:]]/); next
		}
		seen && !stopped {
			if (line == "") next
			if (match(line, /[^[:space:]]/) <= base) { stopped = 1; next }
			sub(/^[[:space:]]+/, "", line)
			print line
		}
	'
}

# line_of REGEX — first line of ci.yml matching the extended regex, or empty.
line_of() { grep -nE "$1" "$ci" | head -1 | cut -d: -f1; }

for job in check design; do
	restore="$(step_block "$job" 'Restore the Go module and build cache')"
	save="$(step_block "$job" 'Save the Go module and build cache')"
	naming="$(step_block "$job" 'Name this job'\''s Go cache')"
	before=$failures

	# 1. One restore step and one save step, pinned by commit, keyed off the naming step — and no
	#    hashFiles in either, which on this runner answers "" rather than failing.
	if [ -z "$restore" ] || [ -z "$save" ]; then
		fail "$job has no restore/save step for its Go cache (restore:[$restore] save:[$save])"
	else
		for pair in "restore:$restore" "save:$save"; do
			block="${pair#*:}"
			kind="${pair%%:*}"
			grep -qE "uses: actions/cache/${kind}@[0-9a-f]{40} # v" <<<"$block" ||
				fail "$job's $kind step is not pinned to a full commit sha with the tag beside it"
			grep -qF 'steps.gocache.outputs.key' <<<"$block" ||
				fail "$job's $kind step names no key from steps.gocache.outputs.key"
			if grep -qF 'hashFiles(' <<<"$block"; then
				fail "$job's $kind step computes its key with hashFiles, which act_runner answers \"\" for on a failure it swallows"
			fi
			if grep -qF 'pkit-go-' <<<"$block"; then
				fail "$job's $kind step writes a literal key into the YAML instead of asking the naming step"
			fi
		done
		[ "$failures" -eq "$before" ] &&
			echo "ok   $job restores and saves its Go cache under the naming step's key, both steps pinned by sha"
	fi

	before=$failures
	# 2. setup-go keeps its hands off the same two paths while these steps exist.
	setup_go="$(uses_block "$job" 'actions/setup-go@')"
	grep -qE '^[[:space:]]+cache: false[[:space:]]*$' <<<"$setup_go" ||
		fail "$job's setup-go no longer says cache: false; it and the two cache steps would own GOMODCACHE/GOCACHE under two keys"
	[ "$failures" -eq "$before" ] &&
		echo "ok   $job's setup-go keeps cache: false: these two steps are the one owner of those paths"

	before=$failures
	# 3. The save is guarded twice over: never after a failed job, never when the archive under this
	#    key is already the one this job just used.
	grep -qF 'success()' <<<"$save" || fail "$job's save step runs after a failed job"
	grep -qF "steps.restore.outputs.cache-hit != 'true'" <<<"$save" ||
		fail "$job's save step re-uploads an archive it just restored: no cache-hit guard"
	[ "$failures" -eq "$before" ] &&
		echo "ok   $job's save step is guarded by success() and by cache-hit != 'true': no re-upload of an unchanged archive"

	before=$failures
	# 4. A miss, an unreachable cache server and a refused upload are cold jobs, not red ticks.
	for pair in "restore:$restore" "save:$save"; do
		grep -qE '^[[:space:]]+continue-on-error: true[[:space:]]*$' <<<"${pair#*:}" ||
			fail "$job's ${pair%%:*} step carries no continue-on-error: a cache the runner cannot reach must not redden the job"
	done
	if grep -qE '^[[:space:]]+fail-on-cache-miss: true' <<<"$restore"; then
		fail "$job's restore step asks for fail-on-cache-miss, the input whose whole job is turning a miss into a failure"
	fi
	[ "$failures" -eq "$before" ] &&
		echo "ok   $job's two cache steps carry continue-on-error and no fail-on-cache-miss: a miss is a cold job"

	before=$failures
	# 5. Both jobs name their cache with the one script, and neither forms a key of its own. Two
	#    recipes that differ by one part are two archives of one build and one cold job per run. Each
	#    caller passes its own job name: a save step runs only when its restore was not an exact hit,
	#    so a key two side-by-side jobs both save under belongs to whichever archive the store kept —
	#    v2 refuses the second reservation of an existing (key, version), v1 keeps the newest
	#    (act/artifactcache/handler_v2.go, handler.go `findExactCache`) — and the loser would restore
	#    a tree built for the other job, hit it exactly, never save and compile cold forever. One file,
	#    one name per caller, and scripts/ci_go_cache_one_saver_per_key_test.sh asks the same question
	#    of the whole workflow rather than of these two steps.
	for pair in "restore:$restore" "save:$save"; do
		grep -qF '${{ steps.gocache.outputs.key }}' <<<"${pair#*:}" ||
			fail "$job's ${pair%%:*} step names no key from steps.gocache.outputs.key"
	done
	if ! grep -qE "^[[:space:]]+run: bash scripts/ci_go_cache_key\.sh ${job}[[:space:]]*\$" <<<"$naming"; then
		fail "$job's naming step does not pass its own job name to scripts/ci_go_cache_key.sh; the key would either have a second owner or be one key two jobs fight over"
	fi
	if grep -qE 'sha256sum|hashFiles\(|go env' <<<"$naming"; then
		fail "$job's naming step digests or hashes something itself instead of calling the script"
	fi
	[ "$failures" -eq "$before" ] &&
		echo "ok   $job forms its key only by running scripts/ci_go_cache_key.sh with its own name, and its two cache steps use that output"

	before=$failures
	# 5b. The path list asks the toolchain where the caches live. A literal /root/.cache/go-build is
	#     right until the job image moves under it — and the archive's version hashes the path list, so
	#     a moved directory would then be a silent miss rather than a restore that unpacked elsewhere.
	grep -qF '${{ steps.gocache.outputs.modcache }}' <<<"$restore" ||
		fail "$job's restore step caches no path read from go env GOMODCACHE"
	grep -qF '${{ steps.gocache.outputs.gocache }}' <<<"$restore" ||
		fail "$job's restore step caches no path read from go env GOCACHE"
	if grep -qE '/(root|home)/' <<<"$restore$save"; then
		fail "$job's cache paths name a home directory literally; ask go env, or a moved image is a permanent miss"
	fi
	[ "$failures" -eq "$before" ] &&
		echo "ok   $job's cache paths are the two the script read from go env, and name no home directory"

	before=$failures
	# 5c. The save's path list is the restore's, entry for entry, in the same order. The archive's
	#     `version` is `sha256(paths.join('|') + compressionMethod + salt)` over the resolved list as
	#     written — `getCacheVersion` in the bundle this workflow pins by sha, dist/save/index.js:43414
	#     and :43427, called on the restore side at dist/restore/index.js:43422 — and the store matches
	#     on (Repo, Key, Version). Two blocks holding the same two entries in the other order are two
	#     different archives: the job restores one, saves the other, is never an exact hit, and is cold
	#     and green forever, re-uploading on every run.
	restore_paths="$(step_paths "$job" 'Restore the Go module and build cache')"
	save_paths="$(step_paths "$job" 'Save the Go module and build cache')"
	if [ -z "$restore_paths" ] || [ "$restore_paths" != "$save_paths" ]; then
		fail "$job's save step caches a different path list from its restore step (restore:[$restore_paths] save:[$save_paths]): the archive it writes is one no restore of this job matches, so it is cold on every run and saves forever"
	else
		echo "ok   $job's save step writes the path list its restore step reads, entry for entry and in that order"
	fi

	before=$failures
	# 5d. The restore asks the store in the right order: this job's own archive first, then any job's
	#     on this toolchain, then any of them on this platform. Most specific last would hand the
	#     first run of a new key the largest archive in the repository instead of its own lineage.
	keys="$(step_restore_keys "$job" 'Restore the Go module and build cache')"
	wanted='${{ steps.gocache.outputs.prefix-job }}
${{ steps.gocache.outputs.prefix-version }}
${{ steps.gocache.outputs.prefix-os }}'
	if [ "$keys" != "$wanted" ]; then
		fail "$job's restore step does not ask its three prefix keys in the order this job's own archive, this toolchain's, this platform's (got [$(tr '\n' ' ' <<<"$keys")])"
	else
		echo "ok   $job's restore asks its prefix keys job, version, os: its own archive before the other job's"
	fi

	before=$failures
	# 5e. Every output these two steps read is one the naming script really writes, by a name the
	#     runner's expression parser can lex. A reference to an output nobody emits is not an error in
	#     that language: getPropertyValue returns nothing at all for a missing map key
	#     (gitea.dev/actionslib pkg/exprparser/interpreter.go:264-315, the parser act_runner 3.3.2 uses),
	#     so the key arrives empty, `if: steps.gocache.outputs.key != ''` skips both cache steps, and the
	#     job runs cold with nothing in the log saying why. A name with a character outside
	#     [A-Za-z0-9_-] would be worse than empty: `lexIdent` breaks an identifier there
	#     (github.com/rhysd/actionlint v1.7.12 expr_lexer.go:263-271 — hyphens are in, which is why these
	#     outputs are named `prefix-job` and not `prefix:job`), and the expression then fails the step.
	refs="$(grep -oE 'steps\.gocache\.[A-Za-z0-9_.-]*' <<<"$restore$save" | sed 's#^steps\.gocache\.##' | sort -u)"
	[ -n "$refs" ] || fail "$job's cache steps read no output of the naming step at all"
	for ref in $refs; do
		case "$ref" in
		outputs.*)
			name="${ref#outputs.}"
			case "$name" in
			*[!A-Za-z0-9_-]*) fail "$job reads steps.gocache.outputs.$name, a name the runner's parser breaks at a character outside [A-Za-z0-9_-]" ;;
			 esac
			grep -qE "^[[:space:]]+echo \"${name}=" <<<"$script" ||
				fail "$job reads steps.gocache.outputs.$name, which $keyscript never writes: the reference is empty, both cache steps skip on it, and the job is cold with no word about why"
			;;
		*) fail "$job's cache steps use $ref; the naming step's id is gocache and the only thing on it is outputs" ;;
		esac
	done
	refs_on_one_line="$(tr '\n' ' ' <<<"${refs//outputs./}")"
	[ "$failures" -eq "$before" ] &&
		echo "ok   every output $job's cache steps read (${refs_on_one_line% }) is one $(basename "$keyscript") writes, by a name the runner's parser lexes"
done

before=$failures
# 5g. The other direction, which only makes sense across both jobs: every output the recipe writes is
#     one some cache step reads. An output nobody reads is a second source of truth about the key — it
#     looks like part of the scheme, it survives the change that made it dead, and it gets copied into
#     the next job that wants a cache. Case 5e refuses a reference to a name that is never written;
#     this refuses a name written and never referenced.
emitted="$(grep -oE '^[[:space:]]+echo "[a-z-]+=' "$keyscript" | sed -E 's/.*"([a-z-]+)=/\1/' | sort -u)"
read_names="$(
	for j in check design; do
		grep -oE 'steps\.gocache\.outputs\.[A-Za-z0-9_-]+' \
			<<<"$(step_block "$j" 'Restore the Go module and build cache')$(step_block "$j" 'Save the Go module and build cache')"
	done | sed 's#.*outputs\.##' | sort -u
)"
unreferenced="$(comm -23 <(printf '%s\n' $emitted) <(printf '%s\n' $read_names))"
unreferenced_flat="$(tr '\n' ' ' <<<"$unreferenced")"
emitted_flat="$(tr '\n' ' ' <<<"$emitted")"
if [ -n "$unreferenced" ]; then
	fail "$keyscript writes ${unreferenced_flat% } to \$GITHUB_OUTPUT and no cache step of either Go job reads it"
else
	echo "ok   every output $(basename "$keyscript") writes is read: ${emitted_flat% }"
fi

before=$failures
# 6. The recipe itself: platform, toolchain, the caller's job name and a digest over both dependency
#    files, three prefix restore keys in that order, and a refusal that cannot redden a job. One file,
#    read once — that is the point of its being a file rather than a `run:` block copied into two jobs.
grep -qF 'go env GOOS GOARCH GOVERSION GOMODCACHE GOCACHE' <<<"$script" ||
	fail "$keyscript does not ask go env for the platform, toolchain and cache paths"
grep -qF 'sha256sum go.mod go.sum' <<<"$script" ||
	fail "$keyscript digests neither go.mod nor go.sum; the key would not change when the dependencies do"
grep -qE '^[[:space:]]+echo "key=pkit-go-\$\{goos\}-\$\{goarch\}-\$\{goversion\}-\$\{job\}-\$\{digest\}"$' <<<"$script" ||
	fail "$keyscript's key is not platform, toolchain, the caller's job name and the digest: two jobs would name one key"
grep -qE '^[[:space:]]+echo "prefix-job=pkit-go-\$\{goos\}-\$\{goarch\}-\$\{goversion\}-\$\{job\}-"$' <<<"$script" ||
	fail "$keyscript's first restore key is not this job's own prefix ending in a dash"
grep -qE '^[[:space:]]+echo "prefix-version=pkit-go-\$\{goos\}-\$\{goarch\}-\$\{goversion\}-"$' <<<"$script" ||
	fail "$keyscript's second restore key is not the version prefix ending in a dash"
grep -qE '^[[:space:]]+echo "prefix-os=pkit-go-\$\{goos\}-\$\{goarch\}-"$' <<<"$script" ||
	fail "$keyscript's third restore key is not the os/arch prefix ending in a dash"
key_parts_order="$(grep -nE '^[[:space:]]+echo "(key|prefix-job|prefix-version|prefix-os)=' <<<"$script" | cut -d: -f1 | tr '\n' ' ')"
if [ "$key_parts_order" != "$(printf '%s\n' $key_parts_order | sort -n | tr '\n' ' ')" ]; then
	fail "$keyscript writes its key parts out of order ($key_parts_order): the ordered prefix list is the order the store is asked in"
fi
grep -qE '^[[:space:]]+exit 0$' <<<"$script_code" ||
	fail "$keyscript has no refusal that exits 0: a cache step must not be able to redden a job"
if grep -qE '\bexit 1\b' <<<"$script_code"; then
	fail "$keyscript can exit 1, which would redden a job over its own cache"
fi
[ "$failures" -eq "$before" ] &&
	echo "ok   $keyscript forms its key from go env, its caller's name and a digest over go.mod and go.sum, and refuses by exiting 0"

# 7. Run it. Everything above reads the recipe; this asks what it answers, in a scratch directory
#    holding copies of the two dependency files, under the argument vector act_runner gives a run step
#    (act/runner/step_run_test.go:65). Four questions: the key's shape, whether two callers get two
#    keys, whether the digest follows go.sum while the restore keys stand, and whether the refusal
#    really emits nothing.
if ! command -v go >/dev/null 2>&1; then
	echo "skip running the key recipe: no go on PATH to ask for GOVERSION, GOMODCACHE and GOCACHE"
else
	run_dir="$(mktemp -d "${TMPDIR:-/tmp}/ci-go-cache-key.XXXXXX")"
	trap 'rm -rf "$run_dir"' EXIT
	cp "$root/go.mod" "$root/go.sum" "$run_dir/"
	(cd "$run_dir" && GITHUB_OUTPUT="$run_dir/out" bash --noprofile --norc -e -o pipefail "$keyscript" check >/dev/null) ||
		fail "$keyscript exited non-zero on a tree it can name"
	key_line="$(grep -m1 '^key=' "$run_dir/out" 2>/dev/null)"
	if grep -qE '^key=pkit-go-[a-z0-9]+-[a-z0-9]+-go[0-9]+(\.[0-9]+){0,2}-check-[0-9a-f]{16}$' <<<"$key_line"; then
		echo "ok   the recipe answers a key of the promised form for the name it was given: ${key_line#key=}"
	else
		fail "$keyscript answered [${key_line:-<no key line>}] against its own shape for the caller named check, with $(wc -l <"$run_dir/out") outputs"
	fi
	for part in modcache gocache key prefix-job prefix-version prefix-os; do
		grep -q "^$part=.\+" "$run_dir/out" ||
			fail "$keyscript emitted no non-empty $part output: the cache steps would restore nothing or everything"
	done
	# The cure for the one key the two jobs used to fight over, measured rather than read: the same
	# tree, the same toolchain, the same dependency digest, and two keys — and both keys still start
	# with the version prefix, so the sharing that was always safe (warming across a change) remains.
	(cd "$run_dir" && GITHUB_OUTPUT="$run_dir/design" bash --noprofile --norc -e -o pipefail "$keyscript" design >/dev/null) ||
		fail "$keyscript exited non-zero naming the design job"
	design_key="$(grep -m1 '^key=' "$run_dir/design" 2>/dev/null)"
	check_prefix="$(grep -m1 '^prefix-version=' "$run_dir/out" 2>/dev/null)"
	key_value="${key_line#*=}"
	design_value="${design_key#*=}"
	version_prefix="${check_prefix#*=}"
	warms_across=0
	case "$design_value" in "${version_prefix}design-"*) warms_across=1 ;; esac
	if [ -z "$design_value" ] || [ "$design_value" = "$key_value" ]; then
		fail "$keyscript gave both Go jobs [$key_line]: whichever archive the store kept would be the one the other restores, cold, forever"
	elif [ "$warms_across" -ne 1 ] ||
		[ "$(grep -m1 '^prefix-job=' "$run_dir/out")" != "prefix-job=${key_value%-*}-" ]; then
		fail "$keyscript's design key [$design_value] does not start with the shared version prefix [$version_prefix], or its first restore key is not its own key without the digest: a dependency change would be a cold build, or would restore the other job's archive before its own lineage"
	else
		echo "ok   the same tree answers two keys, one per caller ($key_value and $design_value), both warming under $version_prefix"
	fi
	echo "// a dependency would land here" >>"$run_dir/go.sum"
	(cd "$run_dir" && GITHUB_OUTPUT="$run_dir/out2" bash --noprofile --norc -e -o pipefail "$keyscript" check >/dev/null)
	after_key="$(grep -m1 '^key=' "$run_dir/out2" 2>/dev/null)"
	after_prefix="$(grep -m1 '^prefix-job=' "$run_dir/out2" 2>/dev/null)"
	expected_prefix="${key_value%-*}-" # the key without its trailing -<16 hex digest>: the job prefix
	if [ -z "$after_key" ] || [ "$after_key" = "$key_line" ]; then
		fail "$keyscript's key survived a go.sum change ([$after_key]): every later commit would take this archive and never save"
	elif [ "${after_prefix#*=}" != "$expected_prefix" ]; then
		fail "$keyscript's restore key did not stay the job prefix ([$after_prefix] expected [$expected_prefix]): a go.sum bump would be a cold build"
	else
		echo "ok   a go.sum change moves the key (${after_key#key=}) and leaves the restore prefix (${after_prefix#prefix-job=})"
	fi
	# No caller name is the same refusal as no go.sum: the job runs cold and green, and no half-named
	# key reaches the store — a key with an empty job part would be one key every job saved under.
	if (cd "$run_dir" && GITHUB_OUTPUT="$run_dir/out4" bash --noprofile --norc -e -o pipefail "$keyscript" >"$run_dir/noslug" 2>&1) &&
		[ ! -s "$run_dir/out4" ] && grep -q '::warning::cannot name this job' "$run_dir/noslug"; then
		echo "ok   with no job name the recipe warns, exits 0 and emits no key, so both cache steps skip"
	else
		fail "$keyscript named a cache for a caller that did not name itself, instead of warning, exiting 0 and emitting nothing: $(cat "$run_dir/noslug" 2>/dev/null)"
	fi
	if (cd "$run_dir" && GITHUB_OUTPUT="$run_dir/out5" bash --noprofile --norc -e -o pipefail "$keyscript" 'De sign: two' >"$run_dir/badslug" 2>&1) &&
		[ ! -s "$run_dir/out5" ] && grep -q '::warning::cannot name this job' "$run_dir/badslug"; then
		echo "ok   a job name that is not a key-safe slug is the same refusal: no key, exit 0"
	else
		fail "$keyscript accepted a job name that is not a slug: $(cat "$run_dir/badslug" 2>/dev/null)"
	fi
	rm -f "$run_dir/go.sum"
	if (cd "$run_dir" && GITHUB_OUTPUT="$run_dir/out3" bash --noprofile --norc -e -o pipefail "$keyscript" check >"$run_dir/warn" 2>&1) &&
		[ ! -s "$run_dir/out3" ] && grep -q '::warning::cannot name this job' "$run_dir/warn"; then
		echo "ok   with go.sum gone the recipe warns, exits 0 and emits no key, so both cache steps skip"
	else
		fail "$keyscript did not refuse a tree it cannot name by warning, exiting 0 and emitting nothing: $(cat "$run_dir/warn" 2>/dev/null)"
	fi
fi

# 8. Test results are not in the archive. Since T-0310 the count both suite goals pass lives in one
#    variable, so the promise is that variable's default together with the two goals that read it:
#    CI sets nothing, so an empty default would put cached test results in the archive it restores.
grep -qE '^TEST_COUNT \?= -count=1$' "$makefile" ||
	fail "Makefile lost the TEST_COUNT default of -count=1: CI sets nothing, so an empty default would put cached test results in the archive CI restores across commits"
race_recipe="$(awk '/^check-race:/ {found = 1} found && /^[a-zA-Z][a-zA-Z0-9_-]*:/ && $0 !~ /^check-race:/ {exit} found {print}' "$makefile")"
grep -qE 'go test -race \$\(TEST_COUNT\)' <<<"$race_recipe" ||
	fail "make check-race no longer interpolates \$(TEST_COUNT): -race stays, but a goal that stops reading the variable stops inheriting the promise its default carries"
check_recipe="$(awk '/^check:/ {found = 1} found && /^[a-zA-Z][a-zA-Z0-9_-]*:/ && $0 !~ /^check:/ {exit} found {print}' "$makefile")"
grep -qF -- '-- $(TEST_COUNT)' <<<"$check_recipe" ||
	fail "make check's gotestsum line no longer interpolates \$(TEST_COUNT), the same promise for the suite itself"
goflags="$(job_text check | grep -E '^[[:space:]]+GOFLAGS:' | head -1 | sed 's/^[[:space:]]*GOFLAGS:[[:space:]]*//')"
if [ "$goflags" = '-p=4' ]; then
	echo "ok   TEST_COUNT defaults to -count=1 and both suite goals read it, and check's GOFLAGS is still only -p=4"
else
	fail "check's job env.GOFLAGS is [$goflags], not -p=4: the parallelism ceiling is not a cache flag, and a GOFLAGS that diluted -count=1 would put test results in the archive"
fi

# 9. Four new steps must not move a gate. The three sibling guards re-assert their own halves; this
#    names the order these steps could disturb.
previous=0
previous_name=''
ordered=1
for pair in \
	"checkout:uses: actions/checkout@" \
	"gocache:name: Name this job's Go cache" \
	"restore:uses: actions/cache/restore@" \
	"make check:run: make check$" \
	"make check-race:run: make check-race$" \
	"govulncheck:run: go run golang.org/x/vuln" \
	"save:uses: actions/cache/save@" \
	"setup-node:uses: actions/setup-node@" \
	"check-e2e-guards:run: make check-e2e-guards$" \
	"make e2e:run: make e2e$" \
	"budget:run: bash scripts/check_budget_ratchet.sh" ; do
	name="${pair%%:*}"
	here="$(line_of "${pair#*:}")"
	if [ -z "$here" ]; then
		fail "ci.yml has no $name step to order (/$name/)"
		ordered=0
		continue
	fi
	if [ "$here" -le "$previous" ]; then
		fail "$name is at line $here, not after $previous_name ($previous)"
		ordered=0
	fi
	previous="$here"
	previous_name="$name"
done
[ "$ordered" -eq 1 ] &&
	echo "ok   the cache steps sit between checkout and the budget verdict without moving a gate, the save with the Go steps it archives"

# 10. The jobs that were left alone stay left alone: mobile.yml and public-consumption.yml still ask
#    setup-go for no cache, which is this brief's scope rather than an oversight.
for workflow in mobile public-consumption; do
	path="$root/.gitea/workflows/$workflow.yml"
	if grep -qE '^[[:space:]]+cache: false[[:space:]]*$' "$path"; then
		echo "ok   $workflow.yml still asks setup-go for no cache (out of this task's scope, and unmeasured)"
	else
		fail "$workflow.yml changed its cache input on this task's authority; it carries no measurement of its own"
	fi
done

# 11. The job that runs the YAML-parsing checks installs a YAML parser. `make check` is a list of
#     commands, and three scripts one of them runs answer their question by reading a workflow as
#     data with `import yaml` — scripts/ci_go_cache_one_saver_per_key_test.sh,
#     scripts/ci_go_cache_job_archives_test.sh and scripts/mobile_journey_fetch_test.sh. This file
#     reads the same document with grep, which is why the walk below looks for the import and not
#     for the filename.
#     The check job's image is pinned to a digest that carries python3 3.12.3 and no PyYAML (measured
#     2026-10-05 by running this repository inside it read-only: `dpkg -l python3-yaml` answers `un`,
#     `import yaml` raises ModuleNotFoundError with exit 1, and the guard above dies on that traceback
#     exactly as the CI step did). Nothing in the tree says the package has to be there, so the question
#     is asked of the tree: walk the goals `make check` runs, its prerequisites included, and every
#     script one of those recipes invokes that goes on to `import yaml` is a script of a job that must
#     install a reader before the step that runs it. A YAML guard added tomorrow is named here by this
#     case; the package taken back out is refused here, the way case 6 of scripts/free_port_test.sh
#     refuses iproute2's removal, and both refusals cost seconds rather than a cold forty minutes.
{
	declare -a queue=(check)
	seen_goals=' '
	yaml_scripts=''
	while [ ${#queue[@]} -gt 0 ]; do
		goal="${queue[0]}"
		queue=("${queue[@]:1}")
		case "$seen_goals" in *" $goal "*) continue ;; esac
		seen_goals="$seen_goals$goal "
		# The goal's own recipe, and the goals it names beside its colon. A target line is
		# `name: deps` or a bare `name:`; a variable is `name = value` or `name := value`, and the
		# test on what follows the colon is what tells the two apart.
		recipe="$(awk -v goal="$goal" '
			substr($0, 1, length(goal) + 1) == goal ":" && substr($0, length(goal) + 2) !~ /^[[:space:]]*=/ { found = 1; next }
			found && $0 !~ /^[[:space:]]/ { found = 0 }
			found { print }
		' "$makefile")"
		for invoked in $(grep -oE '[A-Za-z0-9_./-]+\.(sh|py)' <<<"$recipe" | sort -u); do
			[ -f "$root/$invoked" ] || continue
			# A combined import line names the same dependency: `import glob, os, sys, yaml` loads PyYAML
			# as surely as `import yaml` does, and a reader installed for one spelling is not installed.
			# scripts/mobile_journey_fetch_test.sh, which arrived from main, writes it that way and is a
			# `make check` line, so the narrow regex named two of the three scripts whose parser this job
			# has to install.
			grep -qE '^[[:space:]]*import\b[^#]*\byaml\b' "$root/$invoked" || continue
			case "$yaml_scripts" in *" $invoked "*) ;; *) yaml_scripts="$yaml_scripts$invoked " ;; esac
		done
		prereqs="$(awk -v goal="$goal" '
			substr($0, 1, length(goal) + 1) == goal ":" && substr($0, length(goal) + 2) !~ /^[[:space:]]*=/ {
				sub("^" goal ":[[:space:]]*", "")
				sub(/[[:space:]]*##.*$/, "")
				print
				exit
			}
		' "$makefile")"
		# shellcheck disable=SC2086 # a goal list is words by construction
		queue+=($prereqs)
	done

	# The install line, captured rather than piped into `grep -q`: that combination kills the writer
	# with SIGPIPE once grep has its match, and `set -o pipefail` turns the killed awk into a failed
	# pipeline that says the package is absent when it is present.
	install_line="$(job_text check | grep -E 'apt-get install' || true)"
	if [ -z "$yaml_scripts" ]; then
		echo "ok   no goal make check runs parses YAML, so no job here owes it a parser"
	elif ! grep -qE '(^|[[:space:]])python3-yaml([[:space:]]|$)' <<<"$install_line"; then
		fail "make check parses YAML in [$yaml_scripts] but the check job installs no python3-yaml: the guard dies on ModuleNotFoundError, exit 1, and reports no assertion — the refusal CI gave 5bf518b2 at Makefile:322"
	else
		# The install line inside the `check` job, not merely the first one in the file: another job's
		# reader would not be this one's, and the ordering claim is about the job that runs the goal.
		job_start="$(grep -nE '^[[:space:]]+check:[[:space:]]*$' "$ci" | head -1 | cut -d: -f1 || true)"
		parser_line=''
		for line in $(grep -nE 'apt-get install' "$ci" | cut -d: -f1 || true); do
			if [ -n "$job_start" ] && [ "$line" -gt "$job_start" ]; then parser_line="$line"; break; fi
		done
		check_step_line="$(line_of 'run: make check$')"
		if [ -z "$parser_line" ] || [ -z "$check_step_line" ]; then
			fail "the check job's install step (${parser_line:-none}) or its make check step (${check_step_line:-none}) is not a line of $ci to order"
		elif [ "$parser_line" -ge "$check_step_line" ]; then
			fail "the check job installs its YAML reader at line $parser_line, at or after the make check step at $check_step_line"
		else
			yaml_count=0
			for y in $yaml_scripts; do yaml_count=$((yaml_count + 1)); done
			yaml_list="${yaml_scripts% }"
			echo "ok   the check job installs python3-yaml (line $parser_line) before make check ($check_step_line), whose guards parse YAML in $yaml_count check line(s): ${yaml_list// /, }"
		fi
	fi
}

before=$failures
# 12. Where the save stands decides whether it runs. When a runner goes silent the forge finalises the
#     job as failed and reports every step it had not reached as `failure`, without evaluating any of
#     them: job 51417 (run 50923, 2026-10-05) ends its log with the editor suite's own sampler at
#     23:17:11 and carries `completed_at` 23:31:36, and its save step — which stood behind ~24 minutes
#     of npm, docker and Chromium — is reported failure having never run, so the archive this task
#     exists to write was not written while the Go gates it archives had been green for 25 minutes.
#     The `design` job's copy of the same step, twelve steps earlier in the same run, finished in 9s.
#     What moving it forward gives up is measured beside the step and worth 3.38s: gate 10's
#     `go build -o … ./apps/platformkit` (scripts/e2e.sh:137) and the editor suite's
#     `go run ./tools/designexport` (editor/replacement.test.mjs:1696, 1790, 2013) are two final links
#     over packages `make check`'s own `build:` goal already compiled. The invariant is therefore the
#     two ends of the Go work: after the last step that compiles a package of this repository, before
#     the first that brings in node.
check_job="$(job_text check)"
last_compile="$(grep -nE '^[[:space:]]+run: go run golang.org/x/vuln' <<<"$check_job" | tail -1 | cut -d: -f1)"
job_save="$(grep -nE '^[[:space:]]+uses: actions/cache/save@' <<<"$check_job" | head -1 | cut -d: -f1)"
first_node="$(grep -nE '^[[:space:]]+- uses: actions/setup-node@' <<<"$check_job" | head -1 | cut -d: -f1)"
gate10="$(grep -nE '^[[:space:]]+run: make e2e$' <<<"$check_job" | head -1 | cut -d: -f1)"
for pair in "last Go compile:$last_compile" "save:$job_save" "first node:$first_node" "gate 10:$gate10"; do
	[ -n "${pair#*:}" ] ||
		fail "the check job has no ${pair%%:*} step to order (case 12 cannot tell where the save stands)"
done
if [ -n "$job_save" ] && [ -n "$last_compile" ] && [ -n "$first_node" ] && [ -n "$gate10" ]; then
	if [ "$job_save" -lt "$last_compile" ]; then
		fail "check's save step ($job_save) stands before the Go gates ($last_compile): the archive would hold none of the suite's compilations"
	elif [ "$job_save" -gt "$first_node" ] || [ "$job_save" -gt "$gate10" ]; then
		fail "check's save step ($job_save) stands behind node ($first_node) and gate 10 ($gate10) — ~24 minutes of browser work a job whose runner goes silent never finishes, which is how job 51417 reported this step as failure without running it"
	else
		echo "ok   check's save step ($job_save) follows the last step that compiles ($last_compile) and precedes node ($first_node) and gate 10 ($gate10): the browser tail cannot swallow the archive"
	fi
fi

[ "$failures" -eq 0 ]
