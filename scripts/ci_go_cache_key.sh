#!/usr/bin/env bash
# Name this job's Go cache: ask the toolchain where its caches live, and form the key they answer to.
#
# The key's parts are the platform, the resolved toolchain and a digest over both dependency files:
#
#   pkit-go-${GOOS}-${GOARCH}-${GOVERSION}-$(sha256sum go.mod go.sum | sha256sum | cut -c1-16)
#
# with two prefix restore keys, `pkit-go-${GOOS}-${GOARCH}-${GOVERSION}-` and
# `pkit-go-${GOOS}-${GOARCH}-`, so a go.sum bump warms the build from the archive the previous key
# wrote and a toolchain bump still warms the module download. GOCACHE is content-addressed and
# GOMODCACHE is immutable per module version, so bytes restored across a change are unused bytes
# rather than wrong ones; the store matches on (repository, key, version) with the path list inside
# `version`, which is why the two paths are asked from `go env` here instead of written down — a
# literal /root/.cache/go-build is right until the job image moves under it, and would then be a
# permanent silent miss.
#
# This is a file rather than a `run:` block because the two kernel Go jobs must form one key to share
# one archive, and a step copied into two places is two places to keep in step: `.gitea/workflows/
# ci.yml` calls this twice and scripts/ci_go_cache_test.sh runs it and asserts what it emits.
#
# Why a shell digest and not `hashFiles` in a key expression: on the self-hosted runner hashFiles is a
# node process act_runner spawns inside the job container, and when that fails — copy refused, node
# absent, one-minute ceiling expired — act_runner returns "" with no error at all
# (act/runner/expression.go, act_runner v3.3.2). A silently empty hash freezes the key across commits,
# and every later run then takes an exact hit and never saves: permanently stale, permanently warm in
# the log. Here an empty answer is a visible thing, and it is the refusal below.
#
# The refusal: when go.mod or go.sum is absent, or `go` answers nothing, or the version cannot be
# parsed, this writes no `key` output and exits 0. The two cache steps that read its outputs then skip
# and the job runs cold and green. A cache step is never allowed to redden a job — that is the brief's
# rule, and `make check` refuses a version of this file that can exit 1.
set -eu

read -r goos goarch goversion modcache gocache <<<"$(go env GOOS GOARCH GOVERSION GOMODCACHE GOCACHE | tr '\n' ' ')"
# A channel suffix must not reach a key nothing will match later: a devel host answers
# `go1.27.1-X:nodwarf5`, a CI toolchain answers `go1.27.1`, and a colon in a key is a name no
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
	echo "key=pkit-go-${goos}-${goarch}-${goversion}-${digest}"
	echo "prefix-version=pkit-go-${goos}-${goarch}-${goversion}-"
	echo "prefix-os=pkit-go-${goos}-${goarch}-"
} >>"$GITHUB_OUTPUT"

# The line a run's log is read for: the first run of a new key and the second one that restores it
# differ in nothing else, and `Cache Size:` from the step below names the archive against the ceiling
# the workflow comment owns.
echo "key=pkit-go-${goos}-${goarch}-${goversion}-${digest}"
