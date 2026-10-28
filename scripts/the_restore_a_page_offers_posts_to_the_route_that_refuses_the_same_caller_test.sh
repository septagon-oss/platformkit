#!/usr/bin/env bash
# The address a version panel's restore door posts to is the route that answers that caller.
#
# ui/screens draws the restore door by deriving an address — the record's own path, the row's
# id, the command's verb — while the mount answers the command's route as its own composition
# put it, and httpx.Command has carried a published Endpoint ever since a command learned it
# could live on a surface its resource's reads do not. Those two derivations agreeing is what
# makes the button work, and only a served page and the address it printed can prove it.
#
# The cases beside the panel ask a door's address, its verb and the write count it quotes, and
# stop at the door: whether the address the form names is served, and what it answers to the
# caller the door was refused for, go unasked. This case reads the address out of the served
# page instead of rebuilding it, posts it, and asks both callers of one mount — the caller whose
# command policy carries the verb has the write performed once behind that address, and the
# caller it refuses is refused with the name of that command's own guard, with the write still
# performed just the once.
#
# Three places refuse the second caller as the tree stands: the mounted route's own guard, the
# guard httpx.Resources wraps the callable closure with, and ui/screens' commandPolicy, which
# resolves the command out of the caller's own view. Removing one changes nothing a person sees,
# which is why each half is asked of a copy below rather than assumed impossible: a door drawn to
# an address no route serves, and a door whose write answers a caller the policy refused, are
# both refused there rather than declared safe.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

restore_case=TestTheRestoreDoorAPageDrawsPostsToTheRouteThatRefusesTheSameCaller

# overlay_case <out.json> [<destination>=<source>]... — the JSON cmd/go reads to add one test
# file to ui/screens and, for each copy, to replace the file that draws the door or answers the
# write. An overlaid path is one the build sees and the checkout does not.
overlay_case() {
	local out="$1"; shift
	python3 -c '
import json, os, sys
out, entries = sys.argv[1], sys.argv[2:]
m = {}
for entry in entries:
    dst, src = entry.split("=", 1)
    m[os.path.abspath(dst)] = os.path.abspath(src)
json.dump({"Replace": m}, open(out, "w"))
' "$out" "ui/screens/zz_drawn_restore_route_test.go=$work/drawn_restore_route_test.go" "$@"
}

# rewrite <source> <out> <needle> <replacement> <want-count> — one copy of a file, refused
# outright when the needle is not the number of occurrences the copy was written for, so an edit
# that matched nothing cannot leave a later refusal refusing nothing.
rewrite() {
	python3 - "$1" "$2" "$3" "$4" "$5" <<'PY'
import sys
src, out, needle, replacement, want = sys.argv[1:6]
text = open(src).read()
found, want = text.count(needle), int(want)
if found != want:
    sys.exit("%s: needle found %d time(s), want %d" % (src, found, want))
open(out, "w").write(text.replace(needle, replacement))
PY
}

# refused_of <name> <what-the-copy-does> <overlay-args>... — the case, asked of one copy over the
# overlay. An overlay that could not be written is a failure in its own right: it refuses nothing.
refused_of() {
	local name="$1" what="$2"; shift 2
	if ! overlay_case "$work/$name.json" "$@"; then
		fail "the $name overlay could not be written, so the case below asked nothing"
		return
	fi
	if go test -overlay "$work/$name.json" -count=1 -run "^${restore_case}\$" ./ui/screens </dev/null \
		>"$work/$name.log" 2>&1; then
		fail "the case passes with $what, so it answers to nothing"
	else
		ok "the same case refuses $what"
	fi
}

cat >"$work/drawn_restore_route_test.go" <<'GO'
package screens_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

const drawnRestoreRecord = "/app/note/notes/33333333-3333-4333-8333-333333333333"

// drawnRestoreAction is the address the served page's own restore form posts to, read out of
// that form rather than rebuilt from the record path: a case that rebuilt it would let the page
// and the route pass each other's mistake unseen.
func drawnRestoreAction(t *testing.T, body string) string {
	t.Helper()
	doc := webDOM(t, page.View{Body: []g.Node{g.Raw(body)}})
	for _, form := range webFind(doc, func(n *html.Node) bool { return n.Data == "form" }) {
		action := webAttr(form, "action")
		if strings.HasPrefix(action, drawnRestoreRecord+"/") && strings.Contains(action, "/restore") {
			return action
		}
	}
	t.Fatalf("the record page drew no restore form under its own address %q:\n%s", drawnRestoreRecord, body)
	return ""
}

// TestTheRestoreDoorAPageDrawsPostsToTheRouteThatRefusesTheSameCaller asks the address the
// version panel printed, of the mounted router, twice over one mount whose authorizer answers
// the restore command's own guard by a flag alone. For the caller that flag admits, the drawn
// address is a served route and the command's work is performed exactly once; for the caller it
// refuses, the same address answers 403 naming that command's permission, and the work is still
// performed just the once. Both readings come from one mount and one version series, so a door
// to an address nothing serves, a refusal that is really a 404, and a write that ran for a
// caller the policy refused are each refused here rather than read as an empty page.
func TestTheRestoreDoorAPageDrawsPostsToTheRouteThatRefusesTheSameCaller(t *testing.T) {
	row := map[string]any{"id": "33333333-3333-4333-8333-333333333333", "title": "Buy bread",
		"status": "open", "rank": 2.0, "pinned": false}
	performed := 0
	res := resource()
	res.Get = func(context.Context, uuid.UUID) (map[string]any, error) { return row, nil }
	res.List = func(context.Context, crud.Query) ([]map[string]any, int64, error) {
		return []map[string]any{row}, 1, nil
	}
	res.History = &httpx.History{
		RestoreVerb: "restore",
		Asks:        "This puts that version back.",
		Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
			return []httpx.Version{
				{Version: 9, At: time.Unix(1700000000, 0).UTC(),
					Fields: json.RawMessage(`{"title":"Later title"}`), Changed: []string{"title"}},
				{Version: 3, At: time.Unix(1600000000, 0).UTC(),
					Fields: json.RawMessage(`{"title":"Earlier title"}`), Changed: []string{"title"}},
			}, nil
		},
	}
	res.Commands = []httpx.Command{{Verb: "restore", Summary: "Restore",
		Auth: httpx.Permission("note:act"),
		Run: func(context.Context, uuid.UUID, map[string]any) error {
			performed++
			return nil
		}}}

	admitted := true
	handler := policyServer(t, res, &admitted)
	record := policyRequest(t, handler, http.MethodGet, drawnRestoreRecord)
	if record.Code != http.StatusOK {
		t.Fatalf("the record of a caller the policy admits answers %d, want %d", record.Code, http.StatusOK)
	}
	door := drawnRestoreAction(t, record.Body.String())

	// post asks that one address with a session, the form's own content type and the version the
	// drawn form carried in its hidden field.
	post := func() (int, string) {
		body := strings.NewReader(url.Values{"version": []string{"3"}}.Encode())
		req := httptest.NewRequest(http.MethodPost, "http://"+formHost+door, body)
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		req.Header.Set("Accept", "text/html")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://"+formHost)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out.Code, out.Body.String()
	}

	code, page := post()
	if code >= 400 {
		t.Fatalf("the restore address the page drew for its own caller answers %d, want the write performed: %s", code, page)
	}
	if performed != 1 {
		t.Fatalf("the address the page drew performed the restore %d times, want once", performed)
	}

	admitted = false
	code, page = post()
	if performed != 1 {
		t.Fatalf("the restore write ran for a caller whose own command policy refuses it: %d runs, the address answered %d", performed, code)
	}
	if code != http.StatusForbidden {
		t.Fatalf("the address behind the panel's door answered the refused caller %d, want %d: %s", code, http.StatusForbidden, page)
	}
	if !strings.Contains(page, "note:act") {
		t.Fatalf("the address behind the door refused the caller without naming the command's own guard, which is the reason its door was refused: %d %s", code, page)
	}
}
GO

overlay_case "$work/shipped.json"
if go test -overlay "$work/shipped.json" -count=1 -run "^${restore_case}\$" ./ui/screens </dev/null >"$work/shipped.log" 2>&1; then
	ok "the address the version panel draws is a served route that performs the restore once"
else
	fail "the drawn restore address does not answer its own caller as a served route: $(tail -6 "$work/shipped.log" | tr '\n' ' ')"
fi

# A panel whose door names one address while the mount answers another: the derivation moves by
# one segment, as it would the day a command is composed onto a surface of its own.
if ! rewrite ui/screens/render.go "$work/drift.go" \
	'out.Action = at + "/" + id.String() + "/" + policy.Verb' \
	'out.Action = at + "/" + id.String() + "/" + policy.Verb + "/again"' 1; then
	fail "the moved-door copy could not be written, so the case below asked nothing"
else
	refused_of drift "a restore door drawn to an address the mount does not serve" \
		"ui/screens/render.go=$work/drift.go"
fi

# Neither of the two guards that stand between the drawn door and the command's work: the mounted
# route answers the resource's write permission instead of each command's own, and the callable
# closure is handed to the page with its check neutered. The write must still refuse the caller
# the door refused, and name the guard that refuses them.
if ! rewrite ui/screens/screens.go "$work/routes.go" \
	'Summary: c.Summary, MintsCredential: c.MintsCredential}, c.Auth,' \
	'Summary: c.Summary, MintsCredential: c.MintsCredential}, res.WriteAuth(),' 4; then
	fail "the route-guard copy could not be written, so the case below asked nothing"
elif ! rewrite kit/httpx/schemas.go "$work/closures.go" \
	'if err := a.mayDeclare(ctx, declared); err != nil {' \
	'if err := a.mayDeclare(ctx, declared); err != nil && false {' 2; then
	fail "the unguarded-closure copy could not be written, so the case below asked nothing"
else
	refused_of noguard "a restore write that answers a caller the command policy refused" \
		"ui/screens/screens.go=$work/routes.go" "kit/httpx/schemas.go=$work/closures.go"
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the restore door a page draws leads to the route that answers its caller, and refuses the other: all cases hold'
