#!/usr/bin/env bash
# The administration shell's sign-in page is published whether or not the composition named a door
# for it, and two things have to hold of those descriptors. Nothing else in the tree asks either.
#
# First, no anchor binds an empty target. A binding names the operation it leads to, and one that
# names nothing is refused while the plan is being built. The shell is handed its door by
# `pkit.Get[admincontracts.Signin]`, an optional read, so a composition that names none is a plan
# the kernel accepts and someone has to be able to build.
#
# Second, the anchors follow the door in both directions. Named, the three controls that post
# through it — email, password, submit — are declared, bind the operation the composition named,
# and are each drawn once on the page that leads through it. Not named, none of the three is
# declared, because an anchor exists exactly when its control leads somewhere; the view itself
# stays declared, because the page is mounted either way.
#
# The mutation at the end makes the guard that decides this unconditional again, and the run is
# only accepted when the injected case itself reports the failure: an overlay that stopped
# applying, or a copy that no longer compiles, must not be read as the refusal.
set -euo pipefail
cd "$(dirname "$0")/.."
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

case_name='TestTheAdministrationShellDeclaresASignInDoorOnlyWhenTheCompositionNamedOne'
injected='modules/admin/zz_sign_in_door_test.go'
cat >"$work/case.go" <<'GO'
package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
)

// signInShell composes the administration shell over a dry kernel — no auth module, so
// pkit.Get[admincontracts.Signin] answers with nothing — and returns the descriptors the shell
// published along with the sign-in page it serves. door empty is that composition.
func signInShell(t *testing.T, door string) ([]screen.View, string) {
	t.Helper()
	api, router := httpx.New(httpx.Options{
		Unwired: true, Cache: cache.Memory("pkit"), PublicHost: host, Tenants: caller{},
		Authorize: caller{}, Installation: operatorHost, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
	})
	deps := admin.Deps{Modules: []module.Module{{Name: "note"}}, Authorize: caller{}}
	if door != "" {
		deps.SignIn, deps.SignInOperationID = "/api/v1/auth/login", door
	}
	shell := admin.New(deps)
	api.Declare(nil)
	api.Surfaces("note")
	shell.Routes(api.Surfaces("admin"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the kernel refused the shell's own routes: %v", err)
	}
	code, page, _ := call(t, router, http.MethodGet, "/app/admin/login", "")
	if code != http.StatusOK {
		t.Fatalf("the sign-in page answered %d, want 200", code)
	}
	return shell.Views, page
}

// signInAnchors answers what the sign-in view binds each of its anchors to, and refuses any
// anchor of any view whose submission or value binding names no operation: the plan is built
// before anything is served, and a binding that names nothing is refused there.
func signInAnchors(t *testing.T, views []screen.View, view string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, v := range views {
		for _, a := range v.Anchors() {
			b := a.Binding()
			if (b.Kind == "submission" || b.Kind == "value") && b.Target == "" {
				t.Errorf("%s#%s binds a %s with no operation: the plan would be refused", v.Ref(), a.Name(), b.Kind)
			}
			if string(v.Ref()) == view {
				out[a.Name()] = b.Kind + " " + b.Target
			}
		}
	}
	return out
}

func TestTheAdministrationShellDeclaresASignInDoorOnlyWhenTheCompositionNamedOne(t *testing.T) {
	const signIn = "app/admin/custom/login"
	drawn := regexp.MustCompile(`data-screen-anchor="([^"]+)"`)

	t.Run("the door the composition named", func(t *testing.T) {
		views, page := signInShell(t, "auth-login")
		anchors := signInAnchors(t, views, signIn)
		for name, want := range map[string]string{
			"field.email": "value auth-login", "field.password": "value auth-login",
			"action.submit": "submission auth-login", "action.forgot": "navigation app/admin/custom/forgot",
		} {
			if anchors[name] != want {
				t.Errorf("the sign-in view declares %s as %q, want %s", name, anchors[name], want)
			}
		}
		for name := range anchors {
			n := 0
			for _, m := range drawn.FindAllStringSubmatch(page, -1) {
				if m[1] == signIn+"#"+name {
					n++
				}
			}
			if n != 1 {
				t.Errorf("the served page drew %s#%s %d times, want once", signIn, name, n)
			}
		}
	})

	t.Run("no door named", func(t *testing.T) {
		views, _ := signInShell(t, "")
		if anchors := signInAnchors(t, views, signIn); len(anchors) != 1 {
			t.Errorf("with no door named the sign-in view declares %v, want only the link to the forgot page", anchors)
		}
	})
}
GO

overlay() { # overlay <out.json> [<dest>=<src>]... — the JSON cmd/go reads to read a source elsewhere.
	local out="$1"
	shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
json.dump({"Replace": {os.path.abspath(e.split("=", 1)[0]): os.path.abspath(e.split("=", 1)[1]) for e in entries}}, open(out, "w"))
' "$out" "$@"
}

overlay "$work/clean.json" "$injected=$work/case.go"
if go test -overlay "$work/clean.json" -count=1 -v -run "^${case_name}\$" ./modules/admin </dev/null \
	>"$work/clean.log" 2>&1 && grep -q -- "--- PASS: $case_name" "$work/clean.log"; then
	ok "the shell's sign-in descriptors answer the injected case, and the case reports its own pass"
else
	fail "the run of the tree as it stands reported no verdict for $case_name: $(tail -6 "$work/clean.log" | tr '\n' ' ')"
fi

# The guard that reads the door made unconditional again — the shape the descriptors had before
# the door became an optional read. If the line moves, this case refuses rather than judge a copy
# that was never mutated.
sed 's|if signin.OperationID != "" {|if true {|' modules/admin/contracts/screens.go >"$work/always.go"
if cmp -s modules/admin/contracts/screens.go "$work/always.go"; then
	fail "modules/admin/contracts/screens.go no longer holds the guard this case asks of it in the shape this case reads"
else
	overlay "$work/mutated.json" "$injected=$work/case.go" "modules/admin/contracts/screens.go=$work/always.go"
	go test -overlay "$work/mutated.json" -count=1 -v -run "^${case_name}\$" ./modules/admin </dev/null \
		>"$work/mutated.log" 2>&1 || true
	if grep -q -- "--- FAIL: $case_name" "$work/mutated.log" && ! grep -q "\[build failed\]" "$work/mutated.log"; then
		ok "the descriptors that declare a door the composition never named are refused by the case's own assertion"
	else
		fail "the unconditional copy was not refused by an assertion: $(tail -6 "$work/mutated.log" | tr '\n' ' ')"
	fi
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the administration shell declares a sign-in door only when the composition named one: all cases hold'
