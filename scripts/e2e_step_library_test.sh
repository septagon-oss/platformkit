#!/usr/bin/env bash
# Guard the journey steps this module publishes.
#
# e2e/steps/kernel.ts and e2e/steps/public.ts are a published surface: the version a consumer
# pins that resolves kit/rest and ui/page also carries these two files, and a client's spec
# compiles against the names in them — CHANGELOG's Unreleased paragraph states that promise.
# Nothing else in this repository reads a .ts file: `make check` builds Go, and gate 10 runs a
# browser that transpiles these without asking whether they should exist. So the decisions below
# are made here, in the goal that runs before a merge, beside check-apidiff, which makes the Go
# half of the same promise about exported Go names.
#
# It reads text, so nothing it asks needs node, a browser or a database — the placement argument
# check-run-owner makes in the Makefile. Its cases run over trees built under mktemp -d, in the
# manner of scripts/check_architecture_test.sh, and `decide` below holds every rule: the shipped
# tree and the refused ones are judged by the same function, which is what makes the fake tell
# the truth about the real thing.
#
# Every check loop reads `< <(...)`; none pipes into a `while`. A piped loop sets the refusal flag
# in a subshell, and the guard prints `accept` over its own refusals. That bug was written and
# caught here before any of this was, so the shape is stated rather than left to be rediscovered.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temporary="$(mktemp -d)"
trap 'rm -rf "$temporary"' EXIT

# The published surface, in both directions: a name that vanishes breaks every pinned consumer
# silently, and a name that appears without being declared here first was not decided. `export
# const` and `export type` count as published — SKIP_LINK and Person are names a caller writes.
kernel_names="Person endpoints disposable mailbox login signIn personContext signInAs openSession
signOut useTheme SKIP_LINK StructureOptions structure declaresLanguage moment workspace detail
toggle keyboardSubmit save securityHeaders"
public_names="openHome workspaceEntries signInDoor"

# A step loads where the pin puts it, so the specifiers are a closed set. A relative import that
# climbs out of e2e/steps/ names a file no module zip carries, and the step would not load in the
# checkout that pinned it. The public frame may compose the desk's steps; the desk's file may not
# compose the public one, because the public page is a step of the journey and not the host of it.
imports_ok="@playwright/test"

# Every address is one the reference application serves and the file names as its own:
# apps/platformkit/fault.go pins the workspace root and the sign-in door, modules/auth/internal/
# handler.go:38,49,60 declares the three auth routes, and the homepage is what e2e/site.spec.ts
# reads at the root of the host. A scheme, a host, or a path outside these is one client's address
# written into a file every client imports (decision 0038) — which is the shape of the copy this
# file replaces, whose `/admin/login` and `<client>.localhost` are true of one product only.
addresses_ok="/ /app /app/admin/login /api/v1/auth/login /api/v1/auth/logout /api/v1/auth/me"

# A fallback inside a harness step may name only loopback: the machine the run is on. The literal
# is what text can decide; whether the run was handed an address at all is decided by the step
# itself, which refuses rather than guessing (runURL, disposable, mailbox in e2e/steps/kernel.ts).
# Indirection — a host behind a constant — is out of its reach, and out of any text guard's.
loopback_ok="127.0.0.1 ::1 localhost http://127.0.0.1 http://localhost http://[::1]"

# decide DIR — every rule, over the two published files beneath DIR/e2e/steps. Prints one line per
# refusal and returns non-zero when there was any.
decide() {
	local dir="$1" fail=0 file name declared specifier address fallback exported missing
	for file in "$dir/e2e/steps/kernel.ts" "$dir/e2e/steps/public.ts"; do
		if [[ ! -f "$file" ]]; then
			printf '%s: is missing\n' "${file#"$dir"/}"
			return 1
		fi
		name="${file#"$dir"/}"
		if [[ "$file" == */public.ts ]]; then
			declared=" $(printf '%s' "$public_names" | tr '\n' ' ') "
		else
			declared=" $(printf '%s' "$kernel_names" | tr '\n' ' ') "
		fi

		# The header is the README of a file no README describes. Three things are decidable as
		# text and decision 0022 asks them of every delivery: which application this was written
		# against, what it was composed from, and what is new because nothing existing carried it.
		# A copy that arrived without them says nothing about who owns the facts inside.
		local header
		header="$(awk '/^import /{exit} /^\/\//{print}' "$file")"
		if ! grep -q 'apps/platformkit' <<<"$header"; then
			printf '%s: the header does not say which application it was written against\n' "$name"; fail=1
		fi
		if ! grep -q 'Composed from' <<<"$header"; then
			printf '%s: the header does not say what the steps were composed from\n' "$name"; fail=1
		fi
		if ! grep -q 'Upstream:' <<<"$header" || ! grep -q 'New here' <<<"$header"; then
			printf '%s: the header names no upstream and does not say what is new\n' "$name"; fail=1
		fi

		while read -r specifier; do
			[[ -n "$specifier" ]] || continue
			if [[ "$file" == */public.ts && "$specifier" == './kernel' ]]; then continue; fi
			local allowed=no allowed_import
			for allowed_import in $imports_ok; do
				[[ "$specifier" == "$allowed_import" ]] && allowed=yes
			done
			if [[ "$allowed" != yes ]]; then
				printf '%s: imports %s, which is neither the driver nor the step file it composes: no module zip carries it\n' \
					"$name" "$specifier"; fail=1
			fi
		done < <(grep -oE "from '[^']+'" "$file" | sed "s/^from '//; s/'$//" | sort -u)

		while read -r address; do
			[[ -n "$address" ]] || continue
			local known=no known_address
			for known_address in $addresses_ok; do
				[[ "$address" == "$known_address" ]] && known=yes
			done
			if [[ "$known" != yes ]]; then
				printf '%s: types an address the reference application does not serve: %s\n' "$name" "$address"; fail=1
			fi
		done < <({
			grep -oE "'/[^']*'" "$file" || true
			grep -oE '"/[^"]*"' "$file" | tr -d '"' || true
		} | sed "s/^'//; s/'$//" | sort -u)

		while read -r fallback; do
			[[ -n "$fallback" ]] || continue
			# Only host-shaped literals are asked: a fallback to a sentence is a default, and the
			# thing a step must not invent is a machine.
			[[ "$fallback" == *.* || "$fallback" == *:* ]] || continue
			local host=no loopback
			for loopback in $loopback_ok; do
				[[ "$fallback" == "$loopback" ]] && host=yes
			done
			if [[ "$host" != yes ]]; then
				printf '%s: falls back to a host the run was not handed: %s\n' "$name" "$fallback"; fail=1
			fi
		done < <(grep -oE "([?][?]|\|\|)[[:space:]]*'[^']*'" "$file" | sed -E "s/^.*'(.*)'\$/\1/" |
			grep -v ' ' | sort -u || true)

		# A client's host is not the run's. The reference application is reached at `localhost` on a
		# port scripts/e2e.sh chose; `<client>.localhost` is how a consumer addresses its own tenant,
		# and a file every client imports may not name one of them (decision 0038).
		if grep -qE "'[^']*\.localhost[^']*'" "$file"; then
			printf '%s: names a client host, which the run was not handed\n' "$name"; fail=1
		fi

		# The run's own address, which has no default. e2e/playwright.config.ts:17 keeps the port
		# the harness's own default is; a step that guessed one could drive somebody else's
		# listener, which is what check-run-owner exists to refuse.
		if grep -qE 'PLATFORMKIT_E2E_(URL|MAILPIT_URL)[[:space:]]*[?][?][[:space:]]*[^'"'"'[:space:]]' "$file"; then
			printf '%s: defaults the run'"'"'s address to one of the file'"'"'s own\n' "$name"; fail=1
		fi

		# A secret or a decoder in a file every client loads. The token a mailed journey needs is
		# read out of the inbox the run booted — `mailbox` — and never carried as bytes here.
		if grep -qE '\b(atob|btoa|eval)\(' "$file"; then
			printf '%s: ships a decoder instead of the runtime'"'"'s own\n' "$name"; fail=1
		fi
		if grep -qE "'[0-9a-f]{32,}'" "$file"; then
			printf '%s: names a secret as a literal\n' "$name"; fail=1
		fi

			exported="$(awk -v file="$name" -v declared=" $declared " '
			/^export (async )?(function|const|type) / {
				who = ($2 == "async" ? $4 : $3)
				sub(/[^A-Za-z0-9_$].*$/, "", who)
				if (index(declared, " " who " ") == 0)
					printf "%s: exports a name the composition does not declare: %s\n", file, who
				if (previous !~ /^\/\//)
					printf "%s: exports %s with no leading comment line above it\n", file, who
			}
			{ previous = $0 }
		' "$file")"
		if [[ -n "$exported" ]]; then
			printf '%s\n' "$exported"; fail=1
		fi

		for missing in $declared; do
			if ! grep -qE "^export (async )?(function|const|type) $missing\b" "$file"; then
				printf '%s: no longer exports %s, which a pinned consumer compiles against\n' "$name" "$missing"; fail=1
			fi
		done
	done
	return "$fail"
}

# The control: this tree, as shipped. Without it the cases below only prove the guard fires; the
# control is what proves it accepts the library it guards, and it is the case that failed first.
decide "$root"

# refuses DESCRIPTION EXPECTED TREE — a fixture that must be refused, and refused for the stated
# reason. An unrelated refusal is as wrong as an acceptance: the case would pass without proving
# the rule it names.
refuses() {
	local description="$1" expected="$2" tree="$3" out
	shift 3
	if out="$(decide "$tree" 2>&1)"; then
		printf 'FAIL: %s was accepted\n' "$description" >&2
		exit 1
	fi
	case "$out" in
	*"$expected"*) return 0 ;;
	esac
	printf 'FAIL: %s refused for an unrelated reason:\n%s\n' "$description" "$out" >&2
	exit 1
}

# control CASE — a copy of the shipped tree, so a case cannot touch what it judges.
control() {
	local tree="$temporary/$1"
	mkdir -p "$tree/e2e/steps"
	cp "$root/e2e/steps/kernel.ts" "$root/e2e/steps/public.ts" "$tree/e2e/steps/"
	printf '%s' "$tree"
}

# c1: the consumers' copies as they stand today — the same facts, written here rather than read
# out of a client checkout this repository may not depend on. The run's address defaulted to a
# host of the file's own; the fixture database falls back to a name this harness never writes; the
# driver is reached through a file no module zip carries; the console's own address is typed; and
# there is no header, because a copy never had to explain itself. Refused on every count at once,
# by the function that accepted the two files above: this is the measurement behind "the bytes
# cannot move", not an argument.
mkdir -p "$temporary/c1/e2e/steps"
cat >"$temporary/c1/e2e/steps/kernel.ts" <<'TS'
import { expect } from '@playwright/test';
import { runURL } from '../reporting';

export async function signIn(page: Page, next = '/admin') {
  const base = process.env.PLATFORMKIT_E2E_URL ?? 'http://acme.localhost:8099';
  const database = process.env.PLATFORMKIT_E2E_FIXTURE_DATABASE ?? 'septagon_clients_e2e_1_2';
  await page.goto(`/admin/login?next=${encodeURIComponent(next)}`);
  await expect(page).toHaveURL(/\/admin$/);
}
TS
cat >"$temporary/c1/e2e/steps/public.ts" <<'TS'
import { expect } from '@playwright/test';

export async function signInDoor(page: Page, from: string) {
  await page.goto(from);
  await expect(page).toHaveURL(/\/admin\/login/);
}
TS
refuses 'the copies a consumer kept, unchanged' 'the header does not say which application it was written against' \
	"$temporary/c1"
refuses 'the copies a consumer kept, unchanged: the host' 'falls back to a host the run was not handed: http://acme.localhost:8099' \
	"$temporary/c1"
refuses 'the copies a consumer kept, unchanged: the import' 'imports ../reporting' "$temporary/c1"

tree="$(control c2)"
sed -i "s/ask.host ?? 'localhost'/ask.host ?? 'db.internal'/" "$tree/e2e/steps/kernel.ts"
refuses 'a fallback naming somebody else'"'"'s machine' 'falls back to a host the run was not handed: db.internal' "$tree"

tree="$(control c3)"
sed -i "s#signInPage: '/app/admin/login'#signInPage: '/admin/login'#" "$tree/e2e/steps/kernel.ts"
refuses 'an address the reference application does not serve' 'does not serve: /admin/login' "$tree"

tree="$(control c4)"
printf '// openInvoice is a step somebody added without declaring it.\nexport async function openInvoice(page: Page, id: string) {\n  await page.goto(endpoints.workspace + id);\n}\n' \
	>>"$tree/e2e/steps/kernel.ts"
refuses 'a published name nobody declared' 'exports a name the composition does not declare: openInvoice' "$tree"

tree="$(control c5)"
sed -i "s#  const host = ask.host#  const token = '2f1d4c9be7a0538615fd20c9e4b7a6d3';\n  const host = ask.host#" "$tree/e2e/steps/kernel.ts"
sed -i 's#  return bootstrapped();#  return { email: atob(token), password: bootstrapped().password };#' "$tree/e2e/steps/kernel.ts"
refuses 'a secret as a literal in the shared file' 'names a secret as a literal' "$tree"
refuses 'a decoder in the shared file' 'ships a decoder instead of the runtime'"'"'s own' "$tree"

tree="$(control c6)"
sed -i '/^\/\/ moment is a datetime-local/,/^\/\/ the field a person sees says/d' "$tree/e2e/steps/kernel.ts"
refuses 'a step nobody documented' 'exports moment with no leading comment line above it' "$tree"

tree="$(control c7)"
sed -i '/^export async function save(/,/^}$/d' "$tree/e2e/steps/kernel.ts"
refuses 'a published name that vanished' 'no longer exports save, which a pinned consumer compiles against' "$tree"

tree="$(control c8)"
sed -i "s#from './kernel'#from '../public'#" "$tree/e2e/steps/public.ts"
refuses 'a step file that reaches out of the module zip' 'imports ../public, which is neither the driver nor the step file' "$tree"

printf 'step library: %s and %s publish the surface the composition declares, and load from the pin\n' \
	'e2e/steps/kernel.ts' 'e2e/steps/public.ts'
