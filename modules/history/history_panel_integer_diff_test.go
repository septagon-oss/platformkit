package history_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestTheHistoryPanelPreservesEveryUnitOfAnIntegerChange(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.ExecContext(t.Context(),
		"ALTER TABLE history_memos ADD COLUMN count bigint NOT NULL DEFAULT 0"); err != nil {
		t.Fatal(err)
	}
	api, router := mountedHistorySpec(t, w, rest.Spec[*CountedMemo]{
		Module: "memos", Entity: "memo", Path: "/memo", Read: memoRead, Write: memoWrite,
		History: true, Versions: w.store,
	})
	code, body := call(t, router, acmeHost, http.MethodPost, "/api/v1/memos/memo",
		`{"title":"Exact count","count":9007199254740993}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	var first CountedMemo
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, router, acmeHost, http.MethodPatch, address(first.ID),
		`{"count":9007199254740992}`); code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	versions := w.versionsOf(first.ID)
	if len(versions) != 2 {
		t.Fatalf("the two writes kept %d versions", len(versions))
	}
	for i, expected := range []int64{9007199254740993, 9007199254740992} {
		var stored CountedMemo
		if err := json.Unmarshal([]byte(versions[i].Fields), &stored); err != nil || stored.Count != expected {
			t.Fatalf("version %d holds count %d, want %d: %v", i+1, stored.Count, expected, err)
		}
	}
	resource := api.Resources()[0]
	screens.Mount(api.Surfaces("memos").App, page.Shell{
		Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) },
	}, screens.Options{Workspace: "/app"}, resource)
	panel := servedHistoryPanel(t, router, resource.Screen+"/"+first.ID.String())
	if removed := elementText(panel, "del"); !strings.Contains(removed, "9007199254740993") {
		t.Errorf("the comparison marks %q as removed, want the exact previous integer 9007199254740993", removed)
	}
	if inserted := elementText(panel, "ins"); !strings.Contains(inserted, "9007199254740992") {
		t.Errorf("the comparison marks %q as inserted, want the exact current integer 9007199254740992", inserted)
	}
}
