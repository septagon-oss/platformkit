package resource_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

// parityResource is one generated register carrying each control kind a derived
// screen can be asked to publish: an enum as a select, a richtext body, an
// entity picker, an instant, an integer, a boolean, a textarea, a transport field
// no screen may name, a field its command owns, and three commands — one with an
// argument, one with none over the whole collection, and one whose argument the
// row already holds.
func parityResource() resource.Resource {
	return resource.Resource{
		ScreenOwner: "order", Screen: "/app/order/orders", Immutable: []string{"status"},
		Schema: entity.Schema{Module: "order", Entity: "order", Fields: []entity.Field{
			{Name: "id", Type: entity.TypeString, ReadOnly: true},
			{Name: "title", Type: entity.TypeString, Required: true},
			{Name: "note", Type: entity.TypeString, Widget: "textarea"},
			{Name: "status", Type: entity.TypeString, Enum: []string{"open", "closed"}},
			{Name: "body", Type: entity.TypeString, Widget: "richtext"},
			{Name: "assigneeId", Type: entity.TypeUUID, Widget: "entity-picker"},
			{Name: "dueAt", Type: entity.TypeTime, Widget: "datetime"},
			{Name: "units", Type: entity.TypeInt},
			{Name: "urgent", Type: entity.TypeBool},
			{Name: "transport", Type: entity.TypeString, Widget: "hidden"},
		}},
		Commands: []resource.Command{
			{ScreenOwner: "order", Entity: "order", Verb: "accept", Label: "Accept",
				Fields: []entity.Field{{Name: "reason", Type: entity.TypeString}}},
			{ScreenOwner: "order", Entity: "order", Verb: "reindex", Label: "Reindex", Collection: true},
			{ScreenOwner: "order", Entity: "order", Verb: "copy", Label: "Copy",
				Fields:   []entity.Field{{Name: "from", Type: entity.TypeString}},
				Bindings: map[string]string{"from": "id"}},
			{ScreenOwner: "order", Entity: "order", Verb: "annotate", Label: "Annotate",
				Fields: []entity.Field{{Name: "notes", Type: entity.TypeMap, Elem: entity.TypeString,
					Keys: []entity.MapKey{{Name: "title"}, {Name: "body", Widget: "textarea"}}}}},
		},
	}
}

// anchorsByView maps each data-screen-view on a rendered page to the anchors
// inside it, the way a browser resolves one: an element belongs to the closest
// view it is written into, so a command form under a detail body names its own
// view and not the page around it.
func anchorsByView(t *testing.T, view document.View) map[string][]string {
	t.Helper()
	var out strings.Builder
	if err := g.Group(view.Body).Render(&out); err != nil {
		t.Fatal(err)
	}
	parsed, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	owners := map[string][]string{}
	var walk func(node *html.Node, view string)
	walk = func(node *html.Node, view string) {
		for _, attr := range node.Attr {
			if attr.Key == "data-screen-view" {
				view = attr.Val
			}
		}
		for _, attr := range node.Attr {
			if attr.Key == "data-screen-anchor" {
				if owners[view] == nil {
					owners[view] = []string{}
				}
				owners[view] = append(owners[view], attr.Val)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, view)
		}
	}
	walk(parsed, "")
	for view := range owners {
		slices.Sort(owners[view])
	}
	return owners
}

// TestEveryPublishedAnchorIsDrawnOnceByTheViewThatOwnsIt holds the drawn page and
// the published descriptor to one answer: the anchors a person can be pointed at
// are exactly the controls the renderer drew, one each, under the view that owns
// them. A published name no page prints would leave a consumer pointing at
// nothing, and a drawn control no publication names could never be pointed at.
func TestEveryPublishedAnchorIsDrawnOnceByTheViewThatOwnsIt(t *testing.T) {
	r := parityResource()
	row := map[string]any{"id": "record", "title": "Order", "status": "open"}
	o := resource.Options{}
	rendered := map[string]document.View{
		"app/order/resource/order/list":   resource.List(r, o, []map[string]any{row}, 1, 1, "", true),
		"app/order/resource/order/detail": resource.Detail(r, o, row, true),
		"app/order/resource/order/create": resource.Form(r, o, "/app/order/orders/new", "New order", nil, nil, "", true),
		"app/order/resource/order/edit":   resource.Form(r, o, "/app/order/orders/record", "Edit order", row, nil, "", false),
	}
	published := map[string][]string{}
	for _, v := range resource.ScreenViews(r) {
		names := []string{}
		for _, a := range v.Anchors() {
			names = append(names, string(v.Ref())+"#"+a.Name())
		}
		slices.Sort(names)
		published[string(v.Ref())] = names
	}
	for ref, view := range rendered {
		drawn := anchorsByView(t, view)
		if _, reached := drawn[ref]; !reached {
			t.Errorf("%s was rendered without its own data-screen-view", ref)
			drawn[ref] = []string{}
		}
		for owner, names := range drawn {
			if _, isView := published[owner]; !isView {
				t.Errorf("%s draws %v, which no view publishes", owner, names)
				continue
			}
			want := published[owner]
			got := slices.Clone(names)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("%s draws %v, publishes %v", owner, got, want)
			}
			delete(published, owner)
		}
	}
	for ref, names := range published {
		t.Errorf("%s publishes %v, which no rendered view draws", ref, names)
	}
}

// TestARenderWithoutWriteGrantsDrawsNoPublishedWriteControl checks the same pair
// from the other side: the publication is never narrowed by a caller — it is the
// composition's contract — while the page a read-only caller is shown carries none
// of the controls their routes refuse.
func TestARenderWithoutWriteGrantsDrawsNoPublishedWriteControl(t *testing.T) {
	// A person who may not write is handed no command by the adapter, so the page
	// holds none: kit/httpx narrows the commands by the caller's grants, and this
	// renderer draws only what it was handed.
	r := parityResource()
	r.Commands = nil
	row := map[string]any{"id": "record", "title": "Order", "status": "open"}
	o := resource.Options{}
	for _, view := range []document.View{
		resource.List(r, o, []map[string]any{row}, 1, 1, "", false),
		resource.Detail(r, o, row, false),
	} {
		for owner, names := range anchorsByView(t, view) {
			if len(names) != 0 {
				t.Errorf("a read-only %s draws %v", owner, names)
			}
		}
	}
	if len(resource.ScreenViews(parityResource())) == 0 {
		t.Fatal("a read-only caller narrowed the published composition")
	}
	for _, v := range resource.ScreenViews(parityResource()) {
		if v.Ref().Kind() != "create" && v.Ref().Kind() != "edit" {
			continue
		}
		if _, err := v.Anchor(screen.AnchorRef(string(v.Ref()) + "#action.submit")); err != nil {
			t.Errorf("the published %s view lost its submit: %v", v.Ref().Kind(), err)
		}
	}
}
