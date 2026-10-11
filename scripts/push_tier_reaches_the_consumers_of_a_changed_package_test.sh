#!/usr/bin/env bash
# The push tier runs the packages a diff reaches "directly or through `go list`'s dependency
# graph" (scripts/check_push_tier.sh, CONTRIBUTING.md, decision 0088 rule 5). A change to a
# package that compiles into another package is a change to that other package's behaviour,
# so the selection has to name the consumer and not only the directory the diff touched.
#
# Pinned against a fixture module with the same module path as this repository: `foo` is
# changed, `bar` imports `foo`, `baz` imports nothing. The selection for the push tier must
# name `./foo` and `./bar`, and must not name `./baz` — a selector that lists every package
# would pass the first half and fail the second.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
checker="$root/scripts/test_inventory.py"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid
export GOWORK=off GOFLAGS=-mod=mod

mkdir -p "$fixture/foo" "$fixture/bar" "$fixture/baz" "$fixture/tests"
git -C "$fixture" init -q
module="$(sed -n 's/^module //p' "$root/go.mod")"
printf 'module %s\n\ngo %s\n' "$module" "$(sed -n 's/^go //p' "$root/go.mod")" > "$fixture/go.mod"
cat > "$fixture/foo/foo.go" <<'GO'
package foo

func Foo() int { return 1 }
GO
cat > "$fixture/foo/foo_test.go" <<'GO'
package foo

import "testing"

func TestFoo(t *testing.T) {
	if Foo() != 1 {
		t.Fatal("want 1")
	}
}
GO
cat > "$fixture/bar/bar.go" <<GO
package bar

import "$module/foo"

func Bar() int { return foo.Foo() + 1 }
GO
cat > "$fixture/bar/bar_test.go" <<'GO'
package bar

import "testing"

func TestBar(t *testing.T) {
	if Bar() != 2 {
		t.Fatal("want 2")
	}
}
GO
cat > "$fixture/baz/baz.go" <<'GO'
package baz

func Baz() int { return 3 }
GO
cat > "$fixture/baz/baz_test.go" <<'GO'
package baz

import "testing"

func TestBaz(t *testing.T) {
	if Baz() != 3 {
		t.Fatal("want 3")
	}
}
GO
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: bar imports foo, baz imports nothing'
python3 "$checker" --root "$fixture" --markdown - --write --ceiling 0 >/dev/null
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the inventory'

# The change: one line in foo, the package bar compiles into itself.
cat > "$fixture/foo/foo.go" <<'GO'
package foo

// Foo answers one, and this comment is the diff.
func Foo() int { return 1 }
GO
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: foo changes'

selection="$(python3 "$checker" --root "$fixture" --markdown - --tier push --base HEAD~1)"
fails=0
note() { printf 'FAIL: %s\n' "$*" >&2; fails=$((fails + 1)); }
case " $selection " in
	*" ./foo "*) ;;
	*) note "the changed package itself is not selected: got '$selection'" ;;
esac
case " $selection " in
	*" ./bar "*) ;;
	*) note "the consumer of the changed package is not selected: bar imports foo, got '$selection'" ;;
esac
case " $selection " in
	*" ./baz "*) note "a package that imports nothing the diff touched is selected: got '$selection'" ;;
esac

if [ "$fails" -ne 0 ]; then
	printf '%d assertion(s) about the push tier reaching a changed package'"'"'s consumers failed\n' "$fails" >&2
	exit 1
fi
echo "ok — the push tier selects the changed package and the package that imports it, and nothing else"
