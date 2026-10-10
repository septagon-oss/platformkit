#!/usr/bin/env bash
# A package's tests read more than its Go files: a golden under testdata/ and a
# catalogue under a go:embed directory are inputs to the same test binary. A
# change to either reaches the package, so the push tier runs it — and runs
# nothing for a package the change does not reach.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$(mktemp -d)"
trap 'rm -rf "$fixture"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
export GIT_AUTHOR_NAME=fixture GIT_AUTHOR_EMAIL=f@invalid
export GIT_COMMITTER_NAME=fixture GIT_COMMITTER_EMAIL=f@invalid
export GOWORK=off GOFLAGS=-mod=mod

module="$(sed -n 's/^module //p' "$root/go.mod")"
mkdir -p "$fixture"/{shape/testdata,catalogue/messages,unrelated,tests}
printf 'module %s\n\ngo %s\n' "$module" "$(sed -n 's/^go //p' "$root/go.mod")" > "$fixture/go.mod"

printf 'package shape\n' > "$fixture/shape/shape.go"
printf 'one\n' > "$fixture/shape/testdata/golden.txt"
cat > "$fixture/shape/shape_test.go" <<'GO'
package shape

import (
    "os"
    "testing"
)

func TestGolden(t *testing.T) {
    got, err := os.ReadFile("testdata/golden.txt")
    if err != nil || string(got) != "one\n" {
        t.Fatalf("golden = %q, %v; want one", got, err)
    }
}
GO

cat > "$fixture/catalogue/catalogue.go" <<'GO'
package catalogue

import "embed"

//go:embed messages
var Messages embed.FS
GO
printf '{"greeting":"hello"}\n' > "$fixture/catalogue/messages/en.json"
cat > "$fixture/catalogue/catalogue_test.go" <<'GO'
package catalogue

import "testing"

func TestGreeting(t *testing.T) {
    got, err := Messages.ReadFile("messages/en.json")
    if err != nil || string(got) != "{\"greeting\":\"hello\"}\n" {
        t.Fatalf("en.json = %q, %v; want the greeting", got, err)
    }
}
GO

printf 'package unrelated\n' > "$fixture/unrelated/unrelated.go"
cat > "$fixture/unrelated/unrelated_test.go" <<'GO'
package unrelated

import "testing"

func TestUnrelated(t *testing.T) {
    if 1+1 != 2 {
        t.Fatal("addition changed")
    }
}
GO

git -C "$fixture" init -q
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: a golden, an embedded catalogue, an unrelated package'
python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --write --ceiling 0 >/dev/null 2>&1
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the inventory records the three packages'

fails=0
expect() {
    local want="$1" selection="$2" what="$3"
    case " $selection " in
        *" ./$want "*) ;;
        *) printf 'FAIL: push selection omits %s after %s: [%s]\n' "$want" "$what" "$selection" >&2
           fails=$((fails + 1)) ;;
    esac
    case " $selection " in
        *" ./unrelated "*) printf 'FAIL: push selection includes the unrelated package after %s\n' "$what" >&2
           fails=$((fails + 1)) ;;
    esac
}

# The golden moves; shape's test reads it and fails on the new bytes.
printf 'two\n' > "$fixture/shape/testdata/golden.txt"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the golden changes'
selection="$(python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --tier push --base HEAD~1)"
expect shape "$selection" "a change under shape/testdata"

# The embedded catalogue moves; catalogue's test compiles it and fails on the new bytes.
printf '{"greeting":"goodbye"}\n' > "$fixture/catalogue/messages/en.json"
git -C "$fixture" add -A
git -C "$fixture" commit -qm 'fixture: the embedded catalogue changes'
selection="$(python3 "$root/scripts/test_inventory.py" --root "$fixture" --markdown - --tier push --base HEAD~1)"
expect catalogue "$selection" "a change under catalogue/messages (go:embed)"

if [ "$fails" -ne 0 ]; then exit 1; fi
echo 'ok — push selection reaches the package whose testdata or embedded directory changed, and no other'
