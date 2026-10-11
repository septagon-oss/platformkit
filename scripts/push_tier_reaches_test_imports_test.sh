#!/usr/bin/env bash
# A package consumed only by a test is still an input to that test binary.
# Reuse the inventory fixture-tree pattern: change a dependency, select the
# affected tests, and keep an unrelated package outside the selection.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid
export GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid
export GOWORK=off GOFLAGS=-mod=mod

module="$(sed -n 's/^module //p' "$root/go.mod")"
mkdir -p "$fixture"/{value,internalcase,externalcase,unrelated,tests}
printf 'module %s\n\ngo %s\n' "$module" "$(sed -n 's/^go //p' "$root/go.mod")" > "$fixture/go.mod"
cat > "$fixture/value/value.go" <<'GO'
package value

func Number() int { return 1 }
GO
for pkg in internalcase externalcase unrelated; do
    printf 'package %s\n' "$pkg" > "$fixture/$pkg/value.go"
done
for pkg in internalcase externalcase; do
    test_pkg="$pkg"
    if [ "$pkg" = externalcase ]; then test_pkg="${pkg}_test"; fi
    cat > "$fixture/$pkg/value_test.go" <<GO
package $test_pkg

import (
    "testing"
    "$module/value"
)

func TestNumber(t *testing.T) {
    if got := value.Number(); got != 1 {
        t.Fatalf("number = %d, want 1", got)
    }
}
GO
done
cat > "$fixture/unrelated/value_test.go" <<'GO'
package unrelated

import "testing"

func TestUnrelated(t *testing.T) {
    if 1 + 1 != 2 {
        t.Fatal("addition changed")
    }
}
GO
git -C "$fixture" init -q
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: internal and external tests consume value'
python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --write --ceiling 0 >/dev/null
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: inventory records the consumers'

# This changes both test binaries despite neither production package importing value.
sed -i 's/return 1/return 2/' "$fixture/value/value.go"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: a dependency of two tests changes'
selection="$(python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --tier push --base HEAD~1)"
fails=0
for pkg in internalcase externalcase; do
    case " $selection " in
        *" ./$pkg "*) ;;
        *) printf 'FAIL: push selection omits %s, whose test imports the changed package: [%s]\n' "$pkg" "$selection" >&2
           fails=$((fails + 1)) ;;
    esac
done
case " $selection " in
    *" ./unrelated "*) echo 'FAIL: push selection includes an unrelated package' >&2; fails=$((fails + 1)) ;;
esac
if [ "$fails" -ne 0 ]; then exit 1; fi
echo 'ok — push selection reaches internal and external test imports, excluding unrelated tests'
