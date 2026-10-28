#!/usr/bin/env bash
# The version panel's restore door belongs to the caller the command policy admits.
#
# ui/screens draws the panel beside a record from the caller's resolved command policy:
# view() asks the mounted resource which of its commands this caller may use
# (httpx.Resource.CommandsFor, the same mayUse the route's middleware asks), and
# historyPanel opens a door only for a verb that answer carries. A panel that answered
# from what the resource declares instead would offer a restore to a caller the guard
# refuses — a button whose command answers 403 — and would offer it on the one screen
# where the write puts a stored version back over the row a person is reading.
#
# The four cases that name the panel's door ask its address, the verb at its end and the
# write count it quotes, of a fixture whose guard admits the caller they draw for; none
# of them asks the page of a caller the same guard refuses. This case does, through the
# mounted router, and asks both readings of one mount so the refusal can never be read as
# an empty page. It is injected over Go's build overlay — an overlaid path is one the
# build sees and the checkout does not — and then the same case is asked of a copy whose
# view() keeps the declared commands, which it must refuse.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

panel_case=TestTheServedRecordPageDrawsNoRestoreDoorForACallerWhosePolicyRefusesTheCommand

# overlay_case <out.json> [<destination>=<source>]... — the JSON cmd/go reads to add one
# test file to ui/screens and, for the refusal, to replace the adapter's resolution.
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
' "$out" "ui/screens/zz_panel_command_policy_test.go=$work/panel_policy_test.go" "$@"
}

cat >"$work/panel_policy_test.go" <<'GO'
package screens_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

const policyPanelRecord = "/app/note/notes/11111111-1111-1111-1111-111111111111"

// TestTheServedRecordPageDrawsNoRestoreDoorForACallerWhosePolicyRefusesTheCommand asks
// one mounted record page of one mount, and changes only what the authorizer answers to
// the restore command's own guard. The version series is a read and the restore is a
// write: the caller the policy admits sees the door posting to that record's own restore
// address, and the caller it refuses sees the same panel, the same two version lines and
// the same write count with no door at all. Both readings come from one mount and one
// series, so a refused page, an empty panel or a page that drew no history cannot be
// read as the refusal this asks for.
func TestTheServedRecordPageDrawsNoRestoreDoorForACallerWhosePolicyRefusesTheCommand(t *testing.T) {
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy bread",
		"status": "open", "rank": 2.0, "pinned": false}
	held := func(res *httpx.Resource) {
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
		// The guard is the one the fixture's authorizer answers by flag alone, so the
		// only thing that changes between the two pages is that answer.
		res.Commands = []httpx.Command{{Verb: "restore", Summary: "Restore",
			Auth: httpx.Permission("note:act"),
			Run:  func(context.Context, uuid.UUID, map[string]any) error { return nil }}}
	}

	admitted := true
	record := resource()
	held(&record)
	handler := policyServer(t, record, &admitted)

	page := policyRequest(t, handler, http.MethodGet, policyPanelRecord)
	if page.Code != http.StatusOK {
		t.Fatalf("the record of a caller whose policy offers the restore command answers %d, want %d",
			page.Code, http.StatusOK)
	}
	if !strings.Contains(page.Body.String(), policyPanelRecord+"/restore") {
		t.Fatalf("the record of a caller the policy admits drew no restore door to %q:\n%s",
			policyPanelRecord+"/restore", page.Body.String())
	}

	admitted = false
	refused := policyRequest(t, handler, http.MethodGet, policyPanelRecord)
	if refused.Code != http.StatusOK {
		t.Fatalf("the record of a caller who may not restore answers %d, want %d: the versions are a read this refuses nothing from",
			refused.Code, http.StatusOK)
	}
	body := refused.Body.String()
	for _, door := range []string{policyPanelRecord + "/restore", "/restore "} {
		if strings.Contains(body, door) {
			t.Errorf("the panel offers a restore the caller's own command policy refuses, naming %q on the page", door)
			break
		}
	}
	for _, read := range []string{"Earlier title", "Later title"} {
		if !strings.Contains(body, read) {
			t.Errorf("the record of a caller who may not restore lost the version line %q: the refusal took the read with the write", read)
		}
	}
}
GO

overlay_case "$work/policy.json"
if go test -overlay "$work/policy.json" -count=1 -run "^${panel_case}\$" ./ui/screens </dev/null >"$work/policy.log" 2>&1; then
	ok "the record page draws its restore door from what the caller's policy offers"
else
	fail "the record page does not answer its restore door from the resolved policy: $(tail -6 "$work/policy.log" | tr '\n' ' ')"
fi

# The panel asked of the declarations instead of the caller, in a copy only. An edit that
# matched nothing would leave the refusal below refusing nothing, so the copy is compared
# before it is judged, and the same page must answer both ways.
python3 - "$root/ui/screens/render.go" "$work/declared.go" <<'PY'
import sys
src, out = sys.argv[1], sys.argv[2]
text = open(src).read()
needle = "\tr.Commands = r.CommandsFor(ctx)\n"
if text.count(needle) != 1:
    sys.exit("the caller's reading this case asks about is not the one in ui/screens/render.go")
open(out, "w").write(text.replace(needle, "\t// the declared reading, asked of no caller\n"))
PY
if cmp -s ui/screens/render.go "$work/declared.go"; then
	fail "the declared reading matched nothing in ui/screens/render.go; the case above asked nothing"
elif overlay_case "$work/declared.json" "ui/screens/render.go=$work/declared.go" &&
	go test -overlay "$work/declared.json" -count=1 -run "^${panel_case}\$" ./ui/screens </dev/null >"$work/declared.log" 2>&1; then
	fail "the page still passes with the panel reading what the resource declares: the case answers to nothing"
else
	ok "the same page refuses a panel that reads the declarations beside the caller's answer"
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'the version panel draws its restore door from the caller the command policy admits: all cases hold'
