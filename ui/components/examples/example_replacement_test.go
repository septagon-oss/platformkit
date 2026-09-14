package examples_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

func TestExampleReplacementUsesSourceInputsAndKeepsDestinationMetadata(t *testing.T) {
	calls := 0
	makeSource := func(id, label, content, marker string) examples.Example {
		info := compositionInfo(id, "section")
		info.Name, info.Group = "Label for "+id, "Group for "+id
		return examples.ExampleWithChildren(info, c.TextProps{Content: label},
			[]g.Node{compositionText("body", content).Node}, func(p c.TextProps, children ...g.Node) g.Node {
				calls++
				return g.Group{g.Text(marker + p.Content), g.Group(children)}
			})
	}
	target := makeSource("target/✓", "Old", "old body", "old renderer:")
	source := makeSource("source", "New", "new body", "new renderer:")
	first := examples.ExampleWithChildren(compositionInfo("first", "owner"), c.TextProps{}, []g.Node{nil, target.Node, nil}, func(_ c.TextProps, nodes ...g.Node) g.Node {
		if len(nodes) != 3 || nodes[0] != nil || nodes[2] != nil {
			t.Fatal("replacement changed slot positions or nil members")
		}
		return g.Group{g.Text("owner:"), g.Group(nodes)}
	})
	second := compositionForm("second", target.Node)
	type slots struct{ Body g.Node }
	root := examples.ExampleWithSlots(compositionInfo("root", "shell"), c.FormProps{}, slots{compositionForm("both", first.Node, second.Node).Node},
		func(p c.FormProps, s slots) g.Node { return c.Form(p, s.Body) })
	before, originalSource := describeExample(t, root), describeExample(t, source)
	path := []string{"root", "both", "first", "target/✓"}
	previousCalls := calls
	found, err := root.At(path)
	if err != nil || found.Node != target.Node {
		t.Fatalf("lookup did not return the exact declared capture: %v", err)
	}
	edited, err := root.WithReplacementAt(path, source)
	if err != nil || calls != previousCalls {
		t.Fatalf("replacement rendered or failed: calls %d to %d: %v", previousCalls, calls, err)
	}
	if !slices.Equal(path, []string{"root", "both", "first", "target/✓"}) {
		t.Fatal("replacement mutated its path")
	}
	changed, err := edited.At(path)
	if err != nil {
		t.Fatal(err)
	}
	description := describeExample(t, changed)
	if description.HTML != "new renderer:Newnew body" || description.ExampleInfo != found.ExampleInfo ||
		string(description.Props) != string(originalSource.Props) || description.Children[0].Description.HTML != "new body" {
		t.Fatal("replacement merged old inputs, kept the old renderer or lost destination metadata")
	}
	unchanged, err := edited.At([]string{"root", "both", "second"})
	if err != nil || unchanged.Node != second.Node {
		t.Fatal("replacement rebuilt an unrelated sibling")
	}
	owner, _ := edited.At([]string{"root", "both", "first"})
	if describeExample(t, owner).HTML != "owner:new renderer:Newnew body" {
		t.Fatal("owning slot did not retain its structure")
	}
	if !reflect.DeepEqual(before, describeExample(t, root)) || !reflect.DeepEqual(originalSource, describeExample(t, source)) {
		t.Fatal("replacement mutated either source tree")
	}
	patched, err := changed.WithProps(json.RawMessage(`{"content":"Later"}`))
	if err != nil || describeExample(t, patched).HTML != "new renderer:Laternew body" || describeExample(t, source).HTML != originalSource.HTML {
		t.Fatal("replacement is not an independently editable typed capture")
	}
}

func TestExampleReplacementSharesTheDeclaredInterfaceCheck(t *testing.T) {
	target := compositionText("target", "Before")
	for name, source := range map[string]examples.Example{
		"component identity": examples.ExampleOf(compositionInfo("source", "other"), c.TextProps{}, c.Text),
		"property schema": examples.ExampleOf(compositionInfo("source", "text"), struct {
			Content int `json:"content"`
		}{}, func(struct {
			Content int `json:"content"`
		}) g.Node {
			return nil
		}),
		"slot declaration": examples.ExampleWithChildren(compositionInfo("source", "text"), c.TextProps{}, nil, func(c.TextProps, ...g.Node) g.Node { return nil }),
		"read only":        examples.ExamplePreview(compositionInfo("source", "text"), g.Text("Preview"), "read only"),
	} {
		t.Run(name, func(t *testing.T) {
			edited, err := target.WithReplacementAt([]string{"target"}, source)
			if err == nil || edited.Node != nil {
				t.Fatalf("incompatible %s accepted: %v", name, err)
			}
		})
	}
	left := describeExample(t, target)
	right := left
	right.ID, right.Name, right.Group, right.HTML, right.Props = "other", "Other", "Other", "Other", json.RawMessage(`{}`)
	left.Slots = []examples.SlotDescription{{Name: "first", GoType: "gomponents.Node", Supported: true, TrustedOnly: true}, {Name: "second", GoType: "[]gomponents.Node", Supported: true, Multiple: true, TrustedOnly: true}}
	right.Slots = slices.Clone(left.Slots)
	slices.Reverse(right.Slots)
	if !left.SameInterface(right) || left.Slots[0].Name != "first" || right.Slots[0].Name != "second" {
		t.Fatal("interface comparison depends on content, mutates inputs or uses slot declaration order")
	}
	for _, field := range []string{"name", "type", "support", "multiple", "trust", "editable", "schema", "identity"} {
		changed := right
		changed.Slots = slices.Clone(right.Slots)
		switch field {
		case "name":
			changed.Slots[0].Name = "third"
		case "type":
			changed.Slots[0].GoType = "func() gomponents.Node"
		case "support":
			changed.Slots[0].Supported = false
		case "multiple":
			changed.Slots[0].Multiple = false
		case "trust":
			changed.Slots[0].TrustedOnly = false
		case "editable":
			changed.PropsEditable = false
		case "schema":
			changed.Schema = json.RawMessage(`{}`)
		case "identity":
			changed.ComponentID = "different"
		}
		if left.SameInterface(changed) {
			t.Errorf("interface comparison ignored %s", field)
		}
	}
}

func TestExampleReplacementRefusesForgedCapturesOnEitherSide(t *testing.T) {
	target, source := compositionText("target", "Before"), compositionText("source", "After")
	for _, side := range []string{"target", "source"} {
		for name, mutate := range map[string]func(*examples.Example){
			"id":            func(e *examples.Example) { e.ID = "forged" },
			"interface":     func(e *examples.Example) { e.ComponentID = "forged" },
			"nil":           func(e *examples.Example) { e.Node = nil },
			"opaque":        func(e *examples.Example) { e.Node = g.Text("Forged") },
			"other capture": func(e *examples.Example) { e.Node = compositionText(e.ID, "Forged").Node },
		} {
			t.Run(side+"/"+name, func(t *testing.T) {
				a, b := target, source
				if side == "target" {
					mutate(&a)
				} else {
					mutate(&b)
				}
				if edited, err := a.WithReplacementAt([]string{a.ID}, b); err == nil || edited.Node != nil {
					t.Fatal("replacement laundered a forged capture")
				}
			})
		}
	}
}

func TestExampleReplacementResolvesOnlyExactDeclaredPaths(t *testing.T) {
	leaf := compositionText("leaf/✓", "Before")
	root := compositionForm("root", leaf.Node)
	for _, path := range [][]string{nil, {"ROOT"}, {"root", "children", "leaf/✓"}, {"root", "0"}, {"root", "leaf", "✓"}, {"root", "leaf/✓", "extra"}} {
		if found, err := root.At(path); err == nil || found.Node != nil {
			t.Errorf("lookup accepted %q", path)
		}
		if edited, err := root.WithReplacementAt(path, leaf); err == nil || edited.Node != nil {
			t.Errorf("replacement accepted %q", path)
		}
	}
	for _, nodes := range [][]g.Node{{leaf.Node, g.Text("")}, {leaf.Node, leaf.Node}, {g.Group{leaf.Node}}} {
		ambiguous := compositionForm("root", nodes...)
		if _, err := ambiguous.At([]string{"root", "leaf/✓"}); err == nil {
			t.Fatal("lookup accepted ambiguous ownership")
		}
		if _, err := ambiguous.WithReplacementAt([]string{"root", "leaf/✓"}, leaf); err == nil {
			t.Fatal("replacement accepted ambiguous ownership")
		}
	}
	suppressed := examples.ExampleWithChildren(compositionInfo("root", "owner"), c.TextProps{}, []g.Node{leaf.Node}, func(c.TextProps, ...g.Node) g.Node {
		t.Fatal("source resolution or replacement executed a constructor")
		return nil
	})
	if found, err := suppressed.At([]string{"root", "leaf/✓"}); err != nil || found.Node != leaf.Node {
		t.Fatal("lookup lost a suppressed declared input")
	}
	if _, err := suppressed.WithReplacementAt([]string{"root", "leaf/✓"}, compositionText("new", "After")); err != nil {
		t.Fatal(err)
	}
	if edited, err := leaf.WithReplacementAt([]string{"leaf/✓"}, leaf); err != nil || describeExample(t, edited).HTML != "Before" {
		t.Fatal("self replacement failed")
	}
}

func TestExampleReplacementAcceptsEquivalentGoShapesWithoutAliasingInputs(t *testing.T) {
	type props struct {
		Values map[string][]int `json:"values"`
		Label  string           `json:"label"`
	}
	type destinationSlots struct {
		Body g.Node   `json:"body"`
		Tail []g.Node `json:"tail"`
	}
	type sourceSlots struct {
		Tail []g.Node `json:"tail"`
		Body g.Node   `json:"body"`
	}
	target := examples.ExampleWithSlots(compositionInfo("target", "container"), props{Label: "Old"}, destinationSlots{}, func(props, destinationSlots) g.Node {
		t.Fatal("replacement executed the old renderer")
		return nil
	})
	target.Name, target.Group = "Current display label", "Current display group"
	values := map[string][]int{"source": {1, 2}}
	children := []g.Node{compositionText("tail", "Tail").Node}
	source := examples.ExampleWithSlots(compositionInfo("source", "container"), props{Values: values, Label: "New"},
		sourceSlots{children, compositionText("body", "Body").Node}, func(p props, s sourceSlots) g.Node {
			if !reflect.DeepEqual(p.Values, map[string][]int{"source": {1, 2}}) {
				t.Fatal("source properties aliased caller data or were retained from the target")
			}
			return g.Group{g.Text(p.Label), s.Body, g.Group(s.Tail)}
		})
	edited, err := target.WithReplacementAt([]string{"target"}, source)
	if err != nil {
		t.Fatal(err)
	}
	values["source"][0], children[0] = 99, g.Text("Caller mutation")
	description := describeExample(t, edited)
	if description.ExampleInfo != target.ExampleInfo || description.HTML != "NewBodyTail" || describeExample(t, source).HTML != "NewBodyTail" {
		t.Fatal("equivalent slot interfaces lost metadata, source order or independent inputs")
	}
	// A replacement with duplicate local children is invalid even when its
	// top-level schema and slot declarations match the destination exactly.
	duplicate, err := source.WithSlot("tail", compositionText("body", "Duplicate").Node)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := target.WithReplacementAt([]string{"target"}, duplicate); err == nil || result.Node != nil {
		t.Fatal("replacement admitted ambiguous child identities")
	}
	preview := examples.ExamplePreview(compositionInfo("preview", "container"), g.Text("Preview"), "read only")
	if result, err := preview.WithReplacementAt([]string{"preview"}, source); err == nil || result.Node != nil {
		t.Fatal("replacement converted a read-only preview into a typed capture")
	}
}
