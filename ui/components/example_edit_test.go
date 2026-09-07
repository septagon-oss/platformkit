package components_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func TestExampleWithPropsAtRebuildsOnlyTheNamedOccurrence(t *testing.T) {
	calls := 0
	leaf := c.ExampleOf(compositionInfo("save/✓", "text"), c.TextProps{Content: "Before"}, func(p c.TextProps) g.Node {
		calls++
		return g.Text(p.Content)
	})
	makeOwner := func(id string) c.Example {
		return c.ExampleWithChildren(compositionInfo(id, "form"), c.FormProps{}, []g.Node{nil, leaf.Node, nil}, func(p c.FormProps, nodes ...g.Node) g.Node {
			calls++
			if len(nodes) != 3 || nodes[0] != nil || nodes[2] != nil {
				t.Fatal("editing changed the owning slot's positions or nil members")
			}
			return c.Form(p, nodes...)
		})
	}
	first, second := makeOwner("first"), makeOwner("second")
	type slots struct {
		Body g.Node
		Tail []g.Node
	}
	root := c.ExampleWithSlots(compositionInfo("root", "owner"), c.FormProps{}, slots{first.Node, []g.Node{second.Node}}, func(p c.FormProps, s slots) g.Node {
		calls++
		return c.Form(p, append([]g.Node{s.Body}, s.Tail...)...)
	})
	before := describeExample(t, root)
	path := []string{"root", "first", "save/✓"}
	patch := json.RawMessage(`{"content":"Changed & ✓"}`)
	oldCalls := calls
	edited, err := root.WithPropsAt(path, patch)
	if err != nil {
		t.Fatal(err)
	}
	if calls != oldCalls || string(patch) != `{"content":"Changed & ✓"}` || !slices.Equal(path, []string{"root", "first", "save/✓"}) {
		t.Fatal("editing executed a constructor or mutated caller inputs")
	}
	path[1], patch[0] = "caller mutation", '!'
	after := describeExample(t, edited)
	if after.Children[0].Slot != "Body" || after.Children[1].Slot != "Tail" ||
		after.Children[0].Description.Children[0].Description.HTML != "Changed &amp; ✓" {
		t.Fatal("the exact direct occurrence was not changed through both slot types")
	}
	if !reflect.DeepEqual(before.Children[1].Description, after.Children[1].Description) ||
		!reflect.DeepEqual(before, describeExample(t, root)) || describeExample(t, leaf).HTML != "Before" {
		t.Fatal("an edit mutated the root, shared child capture or sibling occurrence")
	}
	checkCompositionSpan(t, after, after.Children[0])
	checkCompositionSpan(t, after.Children[0].Description, after.Children[0].Description.Children[0])
}

func TestExampleWithPropsAtRootAndTargetOpaqueSlots(t *testing.T) {
	child := c.ExampleWithSlots(compositionInfo("save", "button"), c.ButtonProps{
		ComponentProps: c.ComponentProps{Attrs: map[string]string{"data-trusted": "kept"}},
		HTMXProps:      c.HTMXProps{Post: "/save"}, Label: "Before",
	}, c.ButtonSlots{Content: []g.Node{g.Text("Opaque content")}}, c.ButtonWithSlots)
	child.Name, child.Group = "Display name", "Display group"
	changed, err := child.WithPropsAt([]string{"save"}, json.RawMessage(`{"label":"After"}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := describeExample(t, changed)
	if doc.Name != child.Name || doc.Group != child.Group || !strings.Contains(string(doc.Props), `"label":"After"`) {
		t.Fatal("target-only editing lost display metadata or rejected opaque child slots")
	}
	for _, want := range []string{`data-trusted="kept"`, `hx-post="/save"`, "Opaque content"} {
		if !strings.Contains(doc.HTML, want) {
			t.Fatalf("target-only edit lost trusted input %q", want)
		}
	}
	root := compositionForm("root", child.Node)
	if _, err := root.WithPropsAt([]string{"root", "save"}, json.RawMessage(`{"label":"Nested"}`)); err != nil {
		t.Fatalf("opaque inputs on the target are not traversed: %v", err)
	}
}

func TestExampleWithPropsAtRejectsInvalidPathsAndPatches(t *testing.T) {
	root := compositionForm("root", compositionText("leaf", "Before").Node)
	before := describeExample(t, root)
	for _, path := range [][]string{nil, {}, {"leaf"}, {"ROOT"}, {"root", ""}, {"root", "missing"}, {"root", "children", "leaf"}, {"root", "0"}, {"root", "leaf", "extra"}} {
		if _, err := root.WithPropsAt(path, json.RawMessage(`{"content":"After"}`)); err == nil {
			t.Errorf("accepted an inexact occurrence path %q", path)
		}
	}
	for _, patch := range []string{``, `null`, `[]`, `{"Content":"bad"}`, `{"content":3}`, `{"id":"forged"}`, `{"attrs":{}}`, `{"content":"A","content":"B"}`, `{} {}`} {
		if _, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(patch)); err == nil {
			t.Errorf("accepted invalid nested patch %s", patch)
		}
	}
	if !reflect.DeepEqual(before, describeExample(t, root)) {
		t.Fatal("rejected edits mutated captured inputs")
	}
	preview := c.ExamplePreview(compositionInfo("preview", "helper"), g.Text("Preview"), "read only")
	if _, err := preview.WithPropsAt([]string{"preview"}, json.RawMessage(`{}`)); err == nil {
		t.Fatal("preview acquired an editable Props contract")
	}
}

func TestExampleWithPropsAtKeepsThePublicCaptureGuard(t *testing.T) {
	root := compositionForm("root", compositionText("leaf", "Before").Node)
	for name, mutate := range map[string]func(*c.Example){
		"identity":  func(e *c.Example) { e.ID = "forged" },
		"component": func(e *c.Example) { e.ComponentID = "forged" },
		"nil":       func(e *c.Example) { e.Node = nil },
		"opaque":    func(e *c.Example) { e.Node = g.Text("Replacement") },
		"other capture": func(e *c.Example) {
			e.Node = compositionForm("root", compositionText("leaf", "Other").Node).Node
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := root
			mutate(&changed)
			if _, err := changed.WithPropsAt([]string{changed.ID, "leaf"}, json.RawMessage(`{"content":"After"}`)); err == nil {
				t.Fatal("nested editing laundered a replaced public node or identity")
			}
		})
	}
}

func TestExampleWithPropsAtRefusesAmbiguousOrOpaqueAncestors(t *testing.T) {
	leaf := compositionText("leaf", "Before")
	type slots struct {
		Body     g.Node
		Header   g.Node
		Callback func() g.Node
		Compound []struct{ Node g.Node }
	}
	for name, value := range map[string]slots{
		"wrapped alias": {Body: leaf.Node, Header: g.Group{leaf.Node}},
		"opaque empty":  {Body: leaf.Node, Header: g.Text("")},
		"callback alias": {Body: leaf.Node, Callback: func() g.Node {
			t.Fatal("editing executed an opaque callback")
			return leaf.Node
		}},
		"compound alias":     {Body: leaf.Node, Compound: []struct{ Node g.Node }{{leaf.Node}}},
		"nonzero empty list": {Body: leaf.Node, Compound: []struct{ Node g.Node }{}},
		"duplicate capture":  {Body: leaf.Node, Header: leaf.Node},
		"duplicate identity": {Body: leaf.Node, Header: compositionText("leaf", "Other").Node},
		"only wrapped":       {Body: g.Group{leaf.Node}},
	} {
		t.Run(name, func(t *testing.T) {
			root := c.ExampleWithSlots(compositionInfo("root", "owner"), c.TextProps{}, value, func(c.TextProps, slots) g.Node {
				t.Fatal("validation executed a constructor or tried to discover rendered ownership")
				return nil
			})
			if _, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(`{"content":"After"}`)); err == nil {
				t.Fatal("accepted ambiguous, opaque or non-direct ownership")
			}
		})
	}
	root := c.ExampleWithSlots(compositionInfo("root", "owner"), c.TextProps{}, slots{Body: leaf.Node}, func(c.TextProps, slots) g.Node {
		t.Fatal("an edit rendered the owner")
		return nil
	})
	if _, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(`{"content":"After"}`)); err != nil {
		t.Fatalf("zero unsupported slots must not prevent a declared edit: %v", err)
	}
}

func TestExampleWithPropsAtAllowsSuppressedDeclaredInputWithoutRendering(t *testing.T) {
	leaf := compositionText("leaf", "Before")
	root := c.ExampleWithSlots(compositionInfo("root", "button"), c.ButtonProps{Loading: true, Label: "Wait"}, c.ButtonSlots{IconEnd: []g.Node{leaf.Node}}, c.ButtonWithSlots)
	edited, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(`{"content":"After"}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := describeExample(t, edited)
	if len(doc.Children) != 1 || doc.Children[0].Span != nil || !strings.Contains(string(doc.Children[0].Description.Props), `"content":"After"`) {
		t.Fatal("declared editing was confused with an observed visible edit")
	}
}

func TestExampleWithPropsAtValidatesPortableInputsWithoutExecutingThem(t *testing.T) {
	type props struct {
		Value string `json:"value"`
		Run   func() `json:"run"`
	}
	root := c.ExampleWithChildren(compositionInfo("root", "owner"), props{}, []g.Node{compositionText("leaf", "Before").Node}, func(props, ...g.Node) g.Node {
		t.Fatal("portable validation executed the constructor")
		return nil
	})
	if _, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(`{"content":"After"}`)); err == nil {
		t.Fatal("an ancestor with an unsupported portable schema was rebuilt")
	}
}

func TestExampleWithPropsAtUsesIdentitiesAfterReordering(t *testing.T) {
	first, second := compositionText("first", "A"), compositionText("second", "B")
	root := compositionForm("root", first.Node, second.Node)
	root, err := root.WithSlot("children", second.Node, first.Node)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := root.WithPropsAt([]string{"root", "first"}, json.RawMessage(`{"content":"Changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := describeExample(t, edited)
	if doc.Children[0].Description.ID != "second" || doc.Children[0].Description.HTML != "B" ||
		doc.Children[1].Description.ID != "first" || doc.Children[1].Description.HTML != "Changed" {
		t.Fatal("a property proposal followed an old index instead of the local identity")
	}
}

func TestExampleWithPropsAtKeepsTypedNestedValueSemantics(t *testing.T) {
	type props struct {
		Values map[string][]int `json:"values"`
		Label  string           `json:"label"`
	}
	input := props{Values: map[string][]int{"old": {1, 2}}, Label: "Retained"}
	leaf := c.ExampleOf(compositionInfo("leaf", "data"), input, func(p props) g.Node {
		return g.Text(p.Label)
	})
	root := compositionForm("root", leaf.Node)
	before := describeExample(t, root)
	edited, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(`{"values":{"new":[3,4]}}`))
	if err != nil {
		t.Fatal(err)
	}
	input.Values["old"][0] = 99
	doc := describeExample(t, edited).Children[0].Description
	if string(doc.Props) != `{"label":"Retained","values":{"new":[3,4]}}` || !reflect.DeepEqual(before, describeExample(t, root)) {
		t.Fatal("nested values merged, lost unspecified fields or aliased source inputs")
	}
}

func TestExampleWithPropsAtRejectsDuplicateKeysInsideAny(t *testing.T) {
	type props struct {
		Value any `json:"value"`
	}
	leaf := c.ExampleOf(compositionInfo("leaf", "data"), props{}, func(p props) g.Node {
		if p.Value != nil {
			if _, ok := p.Value.(map[string]any)["large"].(json.Number); !ok {
				t.Error("a valid arbitrary JSON number lost its exact numeric representation")
			}
		}
		return g.Text("Fixture")
	})
	root := compositionForm("root", leaf.Node)
	before := describeExample(t, root)
	for _, patch := range []string{
		`{"value":{"x":1,"x":2}}`,
		`{"value":[{"x":1,"x":2}]}`,
		`{"value":{"array":[{"nested":{"x":1,"x":2}}]}}`,
	} {
		edited, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(patch))
		if err == nil || edited.Node != nil {
			t.Errorf("ambiguous arbitrary JSON was accepted: %s (%v)", patch, err)
		}
	}
	valid := `{"value":{"array":[{"x":1,"y":2},null,true,"ok"],"large":9007199254740993,"nested":{"x":3,"y":4}}}`
	edited, err := root.WithPropsAt([]string{"root", "leaf"}, json.RawMessage(valid))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(describeExample(t, edited).Children[0].Description.Props); got != valid {
		t.Fatalf("valid distinct keys or exact scalar values changed: %s", got)
	}
	if !reflect.DeepEqual(before, describeExample(t, root)) {
		t.Fatal("arbitrary JSON validation or replacement mutated the original capture")
	}
}
