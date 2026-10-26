package rest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/resource"
	g "maragu.dev/gomponents"
)

func TestScreenProjectionPreservesExactAmountsAndQuotedRevisions(t *testing.T) {
	stored := struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Amount   int64  `json:"amount"`
		Revision int64  `json:"revision"`
	}{"one", "Exact record", 9007199254740993, 9007199254740995}
	projected, err := row(stored)
	if err != nil {
		t.Fatal(err)
	}
	if projected["amount"] != json.Number("9007199254740993") || projected["revision"] != json.Number("9007199254740995") {
		t.Fatalf("projection=%v", projected)
	}
	r := resource.Resource{Screen: "/records", Schema: entity.Schema{Module: "probe", Entity: "record", Fields: []entity.Field{
		{Name: "title", Type: entity.TypeString}, {Name: "amount", Type: entity.TypeInt}, {Name: "revision", Type: entity.TypeInt, ReadOnly: true},
	}}, Commands: []resource.Command{{Verb: "confirm", Label: "Confirm", Fields: []entity.Field{{Name: "ifRevision", Type: entity.TypeInt}}, Bindings: map[string]string{"ifRevision": "revision"}}},
		History: &resource.HistoryPanel{Action: "/records/one/restore", Carries: "restore", Condition: display.Text(projected["revision"]), Restore: "Restore", Title: "History", Rows: []resource.VersionRow{{Number: 1}}}}
	o := resource.Options{Workspace: "/", Home: "Home"}
	for name, nodes := range map[string][]g.Node{
		"detail": resource.Detail(r, o, projected, true).Body,
		"edit":   resource.Form(r, o, "/records/one", "Edit record", projected, nil, "", false).Body,
	} {
		var b strings.Builder
		if err := g.Group(nodes).Render(&b); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(b.String(), "9007199254740993") || !strings.Contains(b.String(), `value="9007199254740995"`) {
			t.Fatalf("%s lost digits: %s", name, b.String())
		}
		if strings.Contains(b.String(), "9007199254740992") || strings.Contains(b.String(), "9007199254740996") {
			t.Fatalf("%s rounded a number", name)
		}
	}
}
