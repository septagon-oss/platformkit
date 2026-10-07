#!/usr/bin/env bash
# go_build_retry.sh — run a `go build`, and try it again when the machine's own build
# cache lost an entry out from under it. This file is sourced and never run:
# scripts/rehearse_migrations.sh asks it for the two binaries it needs, and
# scripts/go_build_retry_test.sh is the pin.
#
# Measured reason (this program's gate log of 2026-10-07, the round against `9e696ce`):
# `make check` died in check-rehearse with
#
#	modules/task/contracts/task.go:13:2: could not import context (open
#	  /home/jplr/.cache/go-build/08/088b6284723aab346bd7c558014ee461e8fa550588a4fd6c58e27888d23bb8c7-d:
#	  no such file or directory)
#	modules/tenant/internal/handler.go:170:95: undefined: huma
#	rehearse: the base revision does not build; the copy has to be made by that release's own code
#
# and the same command against the same exported tree — `git archive v1.1.0 | tar -x`,
# then `go build -buildvcs=false -o … ./apps/platformkit` — exited 0 in 5.0s minutes
# later. The tree was not the variable. The sentence that ended the step named a file
# the toolchain had itself written, which is what a build cache miss does *not* look
# like: a miss is a rebuild, and this was a read of an entry the index still listed.
#
# The variable is one cache directory shared by every task worktree on this machine.
# `go` closes *every* command by trimming that cache — $GOROOT's
# src/cmd/go/internal/cache/cache.go holds `func (c *DiskCache) Close() error { return
# c.Trim() }` — and a trim that is more than `trimInterval` (24h here) past the last
# one walks all 256 subdirectories and removes every entry whose "last used" mtime
# precedes `trimLimit` (5 days here). That mtime is refreshed at most once an hour
# (`mtimeInterval`), and only when a command reads the entry. So a long build that
# opened a five-day-old archive, and only opens it again later — which is exactly what
# loading a compiled archive does: the action records the path, the type checker and
# the link open it afterwards — can have the file pulled out from under it by a second
# `go` process finishing elsewhere on the host. Go then reports the archive it was
# handed as missing and every name it carried as `undefined:`, in whichever package
# happened to read it next, which is why one failure named auth, notification, site,
# task, tenant, user and ui/page at once.
#
# Retrying the same command is the cure that costs nothing when nothing is wrong: the
# missing entries are recompiled, and the entries this build wrote itself carry a fresh
# mtime, so the second try needs only what the first one lost. Two waits and it refuses
# — a step that retried without end would be a step nobody could read, and this one
# already has to be able to say "the previous release does not build" when that is true.
#
# What this cannot do is swallow a finding: a compile error names a source file, not
# the cache, and `build_cache_race` below says so, so the first such output is
# forwarded verbatim and its exit code returned. The bound and both verdicts are pinned
# by scripts/go_build_retry_test.sh, which answers `go` itself rather than compiling.
BUILD_RETRY_WAITS=(5 20)

build_cache_race() { # output — exit 0 when the output names a build-cache entry that is gone, 1 when it names a fact about the tree
	local lower cache
	lower="$(printf '%s\n' "${1:-}" | tr '[:upper:]' '[:lower:]')"
	# The missing file has to be in the cache for this to be the machine's fault. The
	# default cache directory's own name is `go-build`, and a run that moved it says so
	# in every one of its own error lines, so `go env GOCACHE` is asked only once a build
	# has already failed and only for this comparison. Anything else that says "no such
	# file or directory" — a source file, an include, a missing module in a vendor tree —
	# names the checkout, and a checkout is the thing under review.
	case "$lower" in
	*"no such file or directory"*) ;;
	*) return 1 ;;
	esac
	case "$lower" in
	*go-build*) return 0 ;;
	esac
	cache="$(go env GOCACHE 2>/dev/null)" || return 1
	cache="$(printf '%s' "$cache" | tr '[:upper:]' '[:lower:]')"
	[ -n "$cache" ] || return 1
	case "$lower" in
	*"$cache"*) return 0 ;;
	esac
	return 1
}

build_retry_wait() { # tries_so_far output — print the seconds to wait before the next try, exit 1 when this failure is final
	local after="$1"
	[ "$after" -lt "${#BUILD_RETRY_WAITS[@]}" ] || return 1
	build_cache_race "${2:-}" || return 1
	printf '%s' "${BUILD_RETRY_WAITS[$after]}"
}

go_build_retry() { # command… — run it, retry a lost cache entry, forward the command's own output either way
	local output rc=0 waits=0 wait last tries
	while :; do
		rc=0
		output="$("$@" 2>&1)" || rc=$?
		if [ "$rc" -eq 0 ]; then
			if [ -n "$output" ]; then
				printf '%s\n' "$output"
			fi
			if [ "$waits" -gt 0 ]; then
				printf 'go build: built on try %s, after %s lost cache entr%s\n' \
					"$((waits + 1))" "$waits" "$([ "$waits" -eq 1 ] && echo y || echo ies)" >&2
			fi
			return 0
		fi
		wait="$(build_retry_wait "$waits" "$output")" || {
			printf '%s\n' "$output" >&2
			return "$rc"
		}
		waits=$((waits + 1))
		tries=$((waits + 1))
		last="$(printf '%s\n' "$output" | grep -v '^[[:space:]]*$' | tail -1)"
		printf 'go build: try %s after the build cache refused an entry it had listed, waiting %ss: %s\n' \
			"$tries" "$wait" "${last:-(no output)}" >&2
		sleep "$wait"
	done
}
