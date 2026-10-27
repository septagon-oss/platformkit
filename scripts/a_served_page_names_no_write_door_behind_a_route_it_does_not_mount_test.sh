#!/usr/bin/env bash
# A page names the doors behind the routes that answer, and nothing else.
#
# ui/screens mounts a write screen only behind writeAvailable: the verb offered, the
# guard declared and the closure wired. The renderer must draw from that same table,
# because a New button, an Edit link or a Delete form behind a route nobody mounted is
# a refusal with a button on it — and its anchor names a view the composition published
# nowhere. The two readings are read from different files (ui/screens/render.go decides
# the table, ui/resource/{list,detail}.go draw the doors), so nothing but a served page
# proves they are one decision.
#
# ui/resource holds the mounted table as a list of verbs, and TestAnAbsentVerbDrawsNoDoor
# reads that list back through the pure renderers; TestScreenDeclarationsExcludeUnavailable
# ExecutableControls reads the published descriptors. Neither asks the mounted adapter for
# its HTML. This case injects one case over Go's build overlay — an overlaid path is one
# the build sees and the checkout does not — asks it of the tree, and then refuses the
# renderer that answers from what the resource declares instead of what the adapter
# mounts, so the case is known to answer to a decision and not to compile alone.
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$root"
failures=0
fail() { echo "FAIL: $1"; failures=$((failures + 1)); }
ok() { echo "ok   $1"; }

served_case=TestAServedPageDrawsNoWriteDoorForAWriteRouteTheAdapterDoesNotMount

# overlay_case <out.json> [<destination>=<source>]... — the JSON cmd/go reads to add one
# test file to ui/screens and, for the refusal, to replace the adapter's route table.
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
' "$out" "ui/screens/zz_served_write_doors_test.go=$work/served_doors_test.go" "$@"
}

cat >"$work/served_doors_test.go" <<'GO'
package screens_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// One record, its collection and its page, and the anchors a drawn door carries.
const (
	servedRecord   = "/app/note/notes/11111111-1111-1111-1111-111111111111"
	servedCreate   = "app/note/resource/note/list#action.create"
	servedEdit     = "app/note/resource/note/detail#action.edit"
	servedDelete   = "app/note/resource/note/detail#action.delete"
	servedRecordID = "11111111-1111-1111-1111-111111111111"
)

// TestAServedPageDrawsNoWriteDoorForAWriteRouteTheAdapterDoesNotMount asks one pair of
// pages of two readings of one resource: it always declares all five verbs, and the
// adapter either holds no write closure — so it mounts no write route — or holds them
// all. Absent the routes, the collection draws no New door and the record no Edit link,
// no delete form and no anchor for either; with them, every door is drawn and carries
// the anchor of the view its screen declared. The pair is asked in one case so an empty
// page can never be read as the correct answer.
func TestAServedPageDrawsNoWriteDoorForAWriteRouteTheAdapterDoesNotMount(t *testing.T) {
	allowed := true
	rows := []map[string]any{{"id": servedRecordID, "title": "Buy milk", "status": "open", "rank": 2.0, "pinned": false}}
	held := func(res *httpx.Resource) {
		res.List = func(context.Context, crud.Query) ([]map[string]any, int64, error) { return rows, 1, nil }
		res.Get = func(context.Context, uuid.UUID) (map[string]any, error) { return rows[0], nil }
	}

	without := resource()
	held(&without)
	without.Create, without.Update, without.Delete = nil, nil, nil
	quiet := policyServer(t, without, &allowed)
	list := policyRequest(t, quiet, http.MethodGet, "/app/note/notes")
	record := policyRequest(t, quiet, http.MethodGet, servedRecord)
	if list.Code != http.StatusOK || record.Code != http.StatusOK {
		t.Fatalf("a read behind a mounted route answers %d and %d", list.Code, record.Code)
	}
	for _, absent := range []string{"/app/note/notes/new", servedRecord + "/edit", servedRecord + "/delete", servedCreate, servedEdit, servedDelete} {
		if strings.Contains(list.Body.String(), absent) {
			t.Errorf("the collection names a write the adapter mounted no route behind: %s", absent)
			break
		}
	}
	for _, absent := range []string{servedRecord + "/edit", servedRecord + "/delete", servedEdit, servedDelete} {
		if strings.Contains(record.Body.String(), absent) {
			t.Errorf("the record names a write the adapter mounted no route behind: %s", absent)
			break
		}
	}

	wired := resource()
	held(&wired)
	open := policyServer(t, wired, &allowed)
	list = policyRequest(t, open, http.MethodGet, "/app/note/notes")
	record = policyRequest(t, open, http.MethodGet, servedRecord)
	if list.Code != http.StatusOK || record.Code != http.StatusOK {
		t.Fatalf("a read of the resource with its writes mounted answers %d and %d", list.Code, record.Code)
	}
	for _, want := range []string{"/app/note/notes/new", servedCreate} {
		if !strings.Contains(list.Body.String(), want) {
			t.Errorf("the collection of a mounted create draws no %s", want)
		}
	}
	for _, want := range []string{servedRecord + "/edit", servedEdit, servedRecord + "/delete", servedDelete} {
		if !strings.Contains(record.Body.String(), want) {
			t.Errorf("the record of a mounted write draws no %s", want)
		}
	}
}
GO

overlay_case "$work/served.json"
if go test -overlay "$work/served.json" -count=1 -run "^${served_case}\$" ./ui/screens </dev/null >"$work/served.log" 2>&1; then
	ok "the mounted collection and record draw a door only where a write route answers"
else
	fail "the served pages do not answer their write doors from the routes: $(tail -6 "$work/served.log" | tr '\n' ' ')"
fi

# The renderer asked of the declarations instead of the routes, in a copy only. An edit
# that matched nothing would leave the refusal below refusing nothing, so the copy is
# compared before it is judged, and the same case must answer the same pages both ways.
sed 's|} else if writeAvailable(r, verb) {|} else if r.Offers(verb) {|' ui/screens/render.go >"$work/declared.go"
if cmp -s ui/screens/render.go "$work/declared.go"; then
	fail "the declared reading matched nothing in ui/screens/render.go; the case above asked nothing"
elif overlay_case "$work/declared.json" "ui/screens/render.go=$work/declared.go" &&
	go test -overlay "$work/declared.json" -count=1 -run "^${served_case}\$" ./ui/screens </dev/null >"$work/declared.log" 2>&1; then
	fail "the pages still pass with the renderer reading what the resource declares: the case answers to nothing"
else
	ok "the same pages refuse a renderer that reads the declarations beside an unwired write closure"
fi

[ "$failures" -eq 0 ] || { echo "$failures case(s) failed"; exit 1; }
echo 'a served page draws its write doors from the routes the adapter mounts: all cases hold'
