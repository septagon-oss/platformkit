#!/usr/bin/env bash
# Name this job's Go cache: ask the toolchain where its caches live, and form the key they answer to.
#
# The caller names the job it is naming the cache for — `bash scripts/ci_go_cache_key.sh design` — and
# the key's parts are the platform, the resolved toolchain, that name, and a digest over both
# dependency files:
#
#   pkit-go-${GOOS}-${GOARCH}-${GOVERSION}-${JOB}-$(sha256sum go.mod go.sum | sha256sum | cut -c1-16)
#
# with three prefix restore keys, most specific first:
#
#   pkit-go-${GOOS}-${GOARCH}-${GOVERSION}-${JOB}-   this job's own archive, whatever the dependency files
#   pkit-go-${GOOS}-${GOARCH}-${GOVERSION}-          any kernel Go job's archive on this toolchain
#   pkit-go-${GOOS}-${GOARCH}-                       any of them on this platform
#
# so a go.sum bump warms a job from its own previous archive, a toolchain bump still warms the module
# download, and the first run of a job's key after either change warms from the other Go job's archive
# of the same toolchain rather than from nothing. GOCACHE is content-addressed and GOMODCACHE is
# immutable per module version, so bytes restored across a change are unused bytes rather than wrong
# ones; the store matches on (repository, key, version) with the path list inside `version`, which is
# why the two paths are asked from `go env` here instead of written down — a literal
# /root/.cache/go-build is right until the job image moves under it, and would then be a permanent
# silent miss.
#
# Why the caller has to name itself. A save step runs only when its restore was not an exact hit, so
# an archive that exists under a key is never saved under again, and the entry the store keeps for a
# key decides which job's build the whole fleet then restores: the v2 cache protocol refuses a second
# reservation of an existing (repository, key, version) and tells the client to skip the upload
# (act/artifactcache/handler_v2.go), the v1 protocol keeps the newest reservation
# (act/artifactcache/handler.go, `findExactCache` and `evictSuperseded`), and a restore stamps
# `UsedAt`, so retention never clears the entry that is read every run. Both kernel Go jobs would
# therefore fight for one shared key, and the loser — `check` on the v2 path, since `design` is
# shorter, and `design` on v1 whenever `check` is red or cancelled — would restore a tree built for
# the other job, take an exact hit, never save its own, and compile cold until go.sum or the toolchain
# moved while the log reported a restore. The two trees are far from the same bytes: `go list -deps
# -test ./...` answers 859 third-party packages over 131 modules for what `check` builds, against 41
# over 8 for `./tools/designexport`, and the build-cache sizes `check`'s own comment records are 781 MB
# and 848 MB against a fraction of that here. One key per job, and the job that finishes first or last
# owns its own archive. What stays shared is the prefix, which is where the warmth actually comes from.
#
# The only sharing that was ever safe is two runs of the *same* job under one key — two refs whose
# go.sum and toolchain match, saving the same bytes: whoever loses the reservation then skips an
# upload of content the winner already wrote. That case does not need a shared key to be harmless.
#
# This is a file rather than a `run:` block because both kernel Go jobs need the same recipe, and a
# recipe copied into two places is two places to keep in step: `.gitea/workflows/ci.yml` calls it with
# each job's own name and scripts/ci_go_cache_test.sh runs it and asserts what it emits.
#
# Why a shell digest and not `hashFiles` in a key expression: on the self-hosted runner hashFiles is a
# node process act_runner spawns inside the job container, and when that fails — copy refused, node
# absent, one-minute ceiling expired — act_runner returns "" with no error at all
# (act/runner/expression.go, act_runner v3.3.2). A silently empty hash freezes the key across commits,
# and every later run then takes an exact hit and never saves: permanently stale, permanently warm in
# the log. Here an empty answer is a visible thing, and it is the refusal below.
#
# The refusal: no job name, a name that is not a key-safe slug, go.mod or go.sum absent, `go` answering
# nothing, or an unparseable version — and this writes no `key` output and exits 0. The two cache steps
# that read its outputs then skip and the job runs cold and green. A cache step is never allowed to
# redden a job — that is the brief's rule, and `make check` refuses a version of this file that can
# exit 1. Six outputs, every one read by the two cache steps: the key and the three prefix restore keys,
# and the two paths their `path:` blocks list. The caller's name is inside the key and inside
# `prefix-job`; it is not emitted as an output of its own, because nothing downstream would read one.
set -eu

# A job name reaches a key nothing will match later if it carries a space, a colon or nothing at all,
# so an argument this rejects is the same refusal as a missing go.sum: cold, green, one warning.
job="${1-}"
if ! printf '%s' "$job" | grep -Eq '^[a-z][a-z0-9-]{0,31}$'; then
	echo "::warning::cannot name this job's Go cache; running cold"
	exit 0
fi

read -r goos goarch goversion modcache gocache <<<"$(go env GOOS GOARCH GOVERSION GOMODCACHE GOCACHE | tr '\n' ' ')"
# A channel suffix must not reach a key nothing will match later: a devel host answers
# `go1.27.1-X:nodwarf5`, a CI toolchain answers `go1.27.2`, and a colon in a key is a name no
# restore key ever repeats.
goversion="$(printf '%s\n' "$goversion" | sed -E 's#^(go[0-9]+(\.[0-9]+){0,2}).*#\1#')"

if [ ! -f go.mod ] || [ ! -f go.sum ] || ! printf '%s' "$goversion" | grep -Eq '^go[0-9]+(\.[0-9]+){0,2}$' ||
	[ -z "$modcache" ] || [ -z "$gocache" ]; then
	echo "::warning::cannot name this job's Go cache; running cold"
	exit 0
fi

digest="$(sha256sum go.mod go.sum | sha256sum | cut -c1-16)"
{
	echo "modcache=$modcache"
	echo "gocache=$gocache"
	echo "key=pkit-go-${goos}-${goarch}-${goversion}-${job}-${digest}"
	echo "prefix-job=pkit-go-${goos}-${goarch}-${goversion}-${job}-"
	echo "prefix-version=pkit-go-${goos}-${goarch}-${goversion}-"
	echo "prefix-os=pkit-go-${goos}-${goarch}-"
} >>"$GITHUB_OUTPUT"

# The line a run's log is read for: the first run of a new key and the second one that restores it
# differ in nothing else, and `Cache Size:` from the step below names the archive against the ceiling
# the workflow comment owns.
echo "key=pkit-go-${goos}-${goarch}-${goversion}-${job}-${digest}"
