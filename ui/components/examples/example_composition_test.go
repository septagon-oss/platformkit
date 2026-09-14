package examples_test

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

func compositionInfo(id, component string) examples.ExampleInfo {
	return examples.ExampleInfo{ID: id, ComponentID: component, Name: id, Group: "Composition"}
}

func compositionText(id, text string) examples.Example {
	return examples.ExampleOf(compositionInfo(id, "text"), c.TextProps{Content: text}, func(p c.TextProps) g.Node {
		return g.Text(p.Content)
	})
}

func compositionForm(id string, children ...g.Node) examples.Example {
	return examples.ExampleWithChildren(compositionInfo(id, "form"), c.FormProps{}, children, c.Form)
}

func checkCompositionSpan(t *testing.T, owner examples.ExampleDescription, child examples.ChildOccurrence) {
	t.Helper()
	span := child.Span
	if span == nil || span.Start < 0 || span.End < span.Start || span.End > len(owner.HTML) {
		t.Fatalf("invalid observed span for %s: %+v", child.Description.ID, span)
	}
	if owner.HTML[span.Start:span.End] != child.Description.HTML {
		t.Fatalf("span does not address the actual child bytes: %+v", child)
	}
}

func TestExampleCompositionJSONRetainsChildInvocation(t *testing.T) {
	button := examples.ExampleOf(examples.ExampleInfo{ID: "save", ComponentID: "button"}, c.ButtonProps{Label: "Save"}, c.Button)
	form := examples.ExampleWithChildren(examples.ExampleInfo{ID: "edit", ComponentID: "form"}, c.FormProps{}, []g.Node{button.Node}, c.Form)
	payload, err := json.Marshal(describeExample(t, form))
	if err != nil {
		t.Fatal(err)
	}
	var projection struct {
		Children []struct {
			Description examples.ExampleDescription
			Slot        string
		}
	}
	if err := json.Unmarshal(payload, &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection.Children) != 1 || projection.Children[0].Description.ID != "save" || projection.Children[0].Slot != "children" {
		t.Fatalf("source child invocation missing from JSON: %+v", projection.Children)
	}
}

func TestExampleCompositionNestedFormsUseLocalIdentitiesAndOneRender(t *testing.T) {
	var calls []string
	makeForm := func(id string) examples.Example {
		var nodes []g.Node
		for _, key := range []string{"save", "cancel"} {
			button := examples.ExampleOf(compositionInfo(key, "button"), c.ButtonProps{Label: "Guardar ✓"}, func(p c.ButtonProps) g.Node {
				calls = append(calls, id+"/"+key)
				return c.Button(p)
			})
			nodes = append(nodes, button.Node)
		}
		return examples.ExampleWithChildren(compositionInfo(id, "form"), c.FormProps{}, nodes, func(p c.FormProps, nodes ...g.Node) g.Node {
			calls = append(calls, id)
			return c.Form(p, nodes...)
		})
	}
	first, second := makeForm("first"), makeForm("second")
	root := examples.ExampleWithChildren(compositionInfo("root", "card"), c.CardProps{}, []g.Node{g.Text("Préface ✓"), first.Node, second.Node}, c.Card)
	if len(calls) != 0 {
		t.Fatal("capture executed a constructor")
	}
	doc := describeExample(t, root)
	if !slices.Equal(calls, []string{"first", "first/save", "first/cancel", "second", "second/save", "second/cancel"}) {
		t.Fatalf("metadata rerendered constructors or changed invocation order: %v", calls)
	}
	if len(doc.Children) != 2 || !slices.Equal(doc.OpaqueSlots, []string{"children"}) {
		t.Fatalf("root ownership missing: %+v", doc)
	}
	for i, name := range []string{"first", "second"} {
		form := doc.Children[i]
		checkCompositionSpan(t, doc, form)
		if form.Description.ID != name || form.Slot != "children" || len(form.Description.Children) != 2 {
			t.Fatalf("nested form identity or ownership changed: %+v", form)
		}
		previousEnd := 0
		for j, key := range []string{"save", "cancel"} {
			button := form.Description.Children[j]
			checkCompositionSpan(t, form.Description, button)
			if button.Description.ID != key || button.Slot != "children" || button.Description.ComponentID != "button" {
				t.Fatalf("repeated definition-local identity lost: %+v", button)
			}
			want := previousEnd + strings.Index(form.Description.HTML[previousEnd:], button.Description.HTML)
			if button.Span.Start != want || !strings.Contains(string(button.Description.Props), "Guardar ✓") {
				t.Fatal("identical labels were matched to the wrong byte occurrence or props")
			}
			previousEnd = button.Span.End
		}
	}
	if doc.Children[0].Span.Start != strings.Index(doc.HTML, doc.Children[0].Description.HTML) {
		t.Fatal("UTF-8 offsets count characters rather than bytes")
	}
	var plain strings.Builder
	if err := root.Node.Render(&plain); err != nil || plain.String() != doc.HTML {
		t.Fatalf("projection changed ordinary HTML rendering: %v", err)
	}
	if again := describeExample(t, root); !reflect.DeepEqual(doc, again) {
		t.Fatal("repeated projection changed source identities or spans")
	}
}

func TestExampleCompositionEditsPreserveIdentityAndCopiedContainers(t *testing.T) {
	first, second := compositionText("first", "A"), compositionText("second", "B")
	input := []g.Node{first.Node, second.Node}
	root := compositionForm("owner", input...)
	before := describeExample(t, root)
	input[0] = g.Text("caller mutation")
	patched, err := first.WithProps(json.RawMessage(`{"content":"Changed ✓"}`))
	if err != nil {
		t.Fatal(err)
	}
	inserted := compositionText("inserted", "New")
	replacement := []g.Node{second.Node, inserted.Node, patched.Node}
	updated, err := root.WithSlot("children", replacement...)
	if err != nil {
		t.Fatal(err)
	}
	replacement[0] = g.Text("replacement mutation")
	updated, err = updated.WithProps(json.RawMessage(`{"Action":"/changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	after := describeExample(t, updated)
	if len(after.Children) != 3 {
		t.Fatalf("replacement child count: %d", len(after.Children))
	}
	for i, id := range []string{"second", "inserted", "first"} {
		checkCompositionSpan(t, after, after.Children[i])
		if after.Children[i].Description.ID != id {
			t.Fatal("reorder or insertion changed a child identity")
		}
	}
	if after.Children[2].Description.HTML != "Changed ✓" || !strings.Contains(after.HTML, `action="/changed"`) {
		t.Fatal("source edits did not reach the same canonical renderer")
	}
	after.Children[0].Description.Props[0] = '!'
	after.Children[0].Span.Start = -1
	if !reflect.DeepEqual(before, describeExample(t, root)) || describeExample(t, updated).Children[0].Span.Start < 0 {
		t.Fatal("projection or caller data aliases the captured source")
	}
	cleared, err := updated.WithSlot("children")
	if err != nil {
		t.Fatal(err)
	}
	if doc := describeExample(t, cleared); len(doc.Children) != 0 || len(doc.OpaqueSlots) != 0 {
		t.Fatal("clearing a slot retained obsolete ownership")
	}
}

func TestExampleCompositionRejectsPublicIdentityAndNodeMutation(t *testing.T) {
	base := examples.ExampleWithSlots(compositionInfo("save", "button"), c.ButtonProps{Label: "Save"}, c.ButtonSlots{}, c.ButtonWithSlots)
	before := describeExample(t, base)
	for name, mutate := range map[string]func(*examples.Example){
		"ID":          func(e *examples.Example) { e.ID = "different" },
		"ComponentID": func(e *examples.Example) { e.ComponentID = "different" },
		"other capture": func(e *examples.Example) {
			e.Node = examples.ExampleOf(e.ExampleInfo, c.ButtonProps{Label: "Different"}, c.Button).Node
		},
		"raw node":      func(e *examples.Example) { e.Node = g.Text("Forged") },
		"nil node":      func(e *examples.Example) { e.Node = nil },
		"function node": func(e *examples.Example) { e.Node = g.NodeFunc(func(io.Writer) error { return nil }) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := base
			mutate(&changed)
			if _, err := changed.Describe(); err == nil {
				t.Fatal("combined changed public data with a stale capture")
			}
			if _, err := changed.WithProps(json.RawMessage(`{"label":"New"}`)); err == nil {
				t.Fatal("WithProps laundered a changed identity or node")
			}
			if _, err := changed.WithSlot("Content", g.Text("New")); err == nil {
				t.Fatal("WithSlot laundered a changed identity or node")
			}
		})
	}
	display := base
	display.Name, display.Group = "New label", "New group"
	doc := describeExample(t, display)
	if doc.Name != display.Name || doc.Group != display.Group || doc.ID != base.ID || doc.HTML != before.HTML {
		t.Fatal("display changes were treated as source identity changes")
	}
	if !reflect.DeepEqual(before, describeExample(t, base)) {
		t.Fatal("rejected changes mutated the original")
	}
}

func TestExampleCompositionOpaqueAndUnobservedAreNotEmpty(t *testing.T) {
	child := compositionText("child", "Visible")
	buffered := g.NodeFunc(func(w io.Writer) error {
		var buffer strings.Builder
		if err := child.Node.Render(&buffer); err != nil {
			return err
		}
		_, err := io.WriteString(w, buffer.String())
		return err
	})
	for _, tc := range []struct {
		name     string
		nodes    []g.Node
		opaque   bool
		observed bool
	}{
		{name: "empty"},
		{name: "nil members", nodes: []g.Node{nil}},
		{name: "empty raw text", nodes: []g.Node{g.Text("")}, opaque: true},
		{name: "empty group", nodes: []g.Node{g.Group{}}, opaque: true},
		{name: "wrapped child", nodes: []g.Node{g.Group{child.Node}}, opaque: true, observed: true},
		{name: "buffered child", nodes: []g.Node{buffered}, opaque: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := describeExample(t, compositionForm("owner", tc.nodes...))
			if slices.Contains(doc.OpaqueSlots, "children") != tc.opaque || len(doc.OpaqueSlots) > 1 {
				t.Fatalf("opaque direct input was confused with an empty slot: %v", doc.OpaqueSlots)
			}
			if tc.observed {
				if len(doc.Children) != 1 || doc.Children[0].Slot != "" {
					t.Fatal("opaque wrapper invented named-slot ownership")
				}
				checkCompositionSpan(t, doc, doc.Children[0])
			} else if len(doc.Children) != 0 {
				t.Fatal("invented an observation through an opaque buffer")
			}
		})
	}
	var childCalls int
	counted := examples.ExampleOf(compositionInfo("hidden", "text"), c.TextProps{Content: "Hidden"}, func(p c.TextProps) g.Node {
		childCalls++
		return g.Text(p.Content)
	})
	button := examples.ExampleWithSlots(compositionInfo("busy", "button"), c.ButtonProps{Label: "Busy", Loading: true}, c.ButtonSlots{IconEnd: []g.Node{counted.Node}}, c.ButtonWithSlots)
	doc := describeExample(t, button)
	if len(doc.Children) != 1 || doc.Children[0].Slot != "IconEnd" || doc.Children[0].Span != nil || childCalls != 0 {
		t.Fatal("declared suppressed child was rerendered or reported observed")
	}
	if !strings.Contains(string(doc.Children[0].Description.Props), "Hidden") || len(doc.OpaqueSlots) != 0 {
		t.Fatal("unobserved source metadata or direct ownership was lost")
	}
}

func TestExampleCompositionObservedEmptyAndDeclarationOrder(t *testing.T) {
	empty := compositionText("empty", "")
	doc := describeExample(t, compositionForm("owner", empty.Node))
	if len(doc.Children) != 1 || doc.Children[0].Span == nil || doc.Children[0].Span.Start != doc.Children[0].Span.End {
		t.Fatal("observed empty output was confused with no observation")
	}
	checkCompositionSpan(t, doc, doc.Children[0])
	first, second := compositionText("first", "A"), compositionText("second", "B")
	type slots struct {
		Body []g.Node
		Tail g.Node
	}
	reversed := examples.ExampleWithSlots(compositionInfo("owner", "reverse"), struct{}{}, slots{[]g.Node{first.Node}, second.Node}, func(_ struct{}, s slots) g.Node {
		return g.Group{s.Tail, g.Group(s.Body)}
	})
	doc = describeExample(t, reversed)
	if len(doc.Children) != 2 || doc.Children[0].Slot != "Body" || doc.Children[1].Slot != "Tail" {
		t.Fatal("observed render order replaced declared slot order")
	}
	for _, child := range doc.Children {
		checkCompositionSpan(t, doc, child)
	}
	if doc.HTML != "BA" || doc.Children[0].Span.Start != 1 || doc.Children[1].Span.Start != 0 {
		t.Fatal("source declaration order was mistaken for actual rendering")
	}
}

func TestExampleCompositionRejectsAmbiguousOccurrencesAndCycles(t *testing.T) {
	child := compositionText("same", "First")
	different := compositionText("same", "Second")
	for name, example := range map[string]examples.Example{
		"same capture twice": compositionForm("owner", child.Node, child.Node),
		"same ID twice":      compositionForm("owner", child.Node, different.Node),
		"one declaration rendered twice": examples.ExampleWithChildren(compositionInfo("owner", "duplicate"), struct{}{}, []g.Node{child.Node}, func(_ struct{}, nodes ...g.Node) g.Node {
			return g.Group{nodes[0], nodes[0]}
		}),
		"unowned repetition": examples.ExampleOf(compositionInfo("owner", "duplicate"), struct{}{}, func(struct{}) g.Node {
			return g.Group{child.Node, child.Node}
		}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := example.Describe(); err == nil {
				t.Fatal("ambiguous occurrence was silently given a positional identity")
			}
		})
	}
	var cycle examples.Example
	cycle = examples.ExampleOf(compositionInfo("cycle", "recursive"), struct{}{}, func(struct{}) g.Node { return cycle.Node })
	if _, err := cycle.Describe(); err == nil {
		t.Fatal("actual bound-node render cycle was accepted")
	}
	if doc := describeExample(t, compositionForm("healthy", child.Node)); len(doc.Children) != 1 {
		t.Fatal("failed capture poisoned a later independent observation")
	}
}

func TestExampleCompositionPropagatesFailureAfterPartialChildWrite(t *testing.T) {
	failure := errors.New("child failed after bytes")
	var calls int
	child := examples.ExampleOf(compositionInfo("child", "failing"), struct{}{}, func(struct{}) g.Node {
		calls++
		return g.NodeFunc(func(w io.Writer) error {
			if _, err := io.WriteString(w, "partial ✓"); err != nil {
				return err
			}
			return failure
		})
	})
	if _, err := compositionForm("owner", child.Node).Describe(); !errors.Is(err, failure) || calls != 1 {
		t.Fatalf("partial rendering hid its error or reran the constructor: calls=%d, error=%v", calls, err)
	}
}

func TestExampleCompositionRejectsSwallowedObservationErrors(t *testing.T) {
	child := compositionText("child", "Text")
	duplicate := examples.ExampleWithChildren(compositionInfo("owner", "duplicate"), struct{}{}, []g.Node{child.Node}, func(_ struct{}, nodes ...g.Node) g.Node {
		return g.NodeFunc(func(w io.Writer) error {
			if err := nodes[0].Render(w); err != nil {
				return err
			}
			_ = nodes[0].Render(w)
			return nil
		})
	})
	var cycle examples.Example
	cycle = examples.ExampleOf(compositionInfo("cycle", "recursive"), struct{}{}, func(struct{}) g.Node {
		return g.NodeFunc(func(w io.Writer) error {
			_ = cycle.Node.Render(w)
			return nil
		})
	})
	for name, example := range map[string]examples.Example{"duplicate": duplicate, "cycle": cycle} {
		t.Run(name, func(t *testing.T) {
			if _, err := example.Describe(); err == nil {
				t.Fatal("renderer concealed invalid ownership by swallowing its error")
			}
		})
	}
}

func TestExampleCompositionSharedCaptureIsNotACycle(t *testing.T) {
	child := compositionText("local", "Shared")
	left, right := compositionForm("left", child.Node), compositionForm("right", child.Node)
	doc := describeExample(t, compositionForm("root", left.Node, right.Node))
	if len(doc.Children) != 2 {
		t.Fatal("independent parent occurrences were merged")
	}
	for _, parent := range doc.Children {
		if len(parent.Description.Children) != 1 || parent.Description.Children[0].Description.ID != "local" {
			t.Fatal("shared capture was rejected or assigned a global occurrence ID")
		}
		checkCompositionSpan(t, doc, parent)
		checkCompositionSpan(t, parent.Description, parent.Description.Children[0])
	}
}

func TestExampleCompositionRejectsMissingChildIdentity(t *testing.T) {
	for _, info := range []examples.ExampleInfo{{ID: "child"}, {ComponentID: "text"}, {ID: " ", ComponentID: "text"}} {
		child := examples.ExampleOf(info, c.TextProps{Content: "Child"}, c.Text)
		if _, err := compositionForm("owner", child.Node).Describe(); err == nil {
			t.Fatalf("accepted child without stable source identity: %+v", info)
		}
	}
}

func TestExampleCompositionRejectsRecoveredChildPanic(t *testing.T) {
	defer func() {
		if value := recover(); value != nil {
			t.Fatalf("observation leaked a panic after the parent recovered: %v", value)
		}
	}()
	failure := errors.New("child rendering panicked")
	child := examples.ExampleOf(compositionInfo("child", "failing"), struct{}{}, func(struct{}) g.Node {
		return g.NodeFunc(func(w io.Writer) error {
			if _, err := io.WriteString(w, "partial"); err != nil {
				return err
			}
			panic(failure)
		})
	})
	var recovered any
	parent := examples.ExampleWithChildren(compositionInfo("owner", "recovering"), struct{}{}, []g.Node{child.Node}, func(_ struct{}, nodes ...g.Node) g.Node {
		return g.NodeFunc(func(w io.Writer) error {
			if _, err := io.WriteString(w, "prefix"); err != nil {
				return err
			}
			func() {
				defer func() { recovered = recover() }()
				_ = nodes[0].Render(w)
			}()
			_, err := io.WriteString(w, "suffix")
			return err
		})
	})
	description, err := parent.Describe()
	if recovered != failure {
		t.Fatalf("recorder changed the panic delivered to the parent: %v", recovered)
	}
	if err == nil || !reflect.DeepEqual(description, examples.ExampleDescription{}) {
		t.Fatalf("recovered child panic certified incomplete rendering: description=%+v, error=%v", description, err)
	}
}

func TestExampleCompositionDeclaredSlotDoesNotProveFieldUse(t *testing.T) {
	child := compositionText("child", "Only the body renders")
	type slots struct {
		Header g.Node
		Body   g.Node
	}
	parent := examples.ExampleWithSlots(compositionInfo("owner", "alias"), struct{}{}, slots{
		Header: child.Node,
		Body:   g.Group{child.Node},
	}, func(_ struct{}, supplied slots) g.Node { return supplied.Body })
	doc := describeExample(t, parent)
	if len(doc.Children) != 1 || doc.Children[0].Slot != "Header" || !slices.Equal(doc.OpaqueSlots, []string{"Body"}) {
		t.Fatalf("declaration ownership and opaque content were conflated: %+v", doc)
	}
	// The same capture was declared in Header but emitted through opaque Body.
	// Slot plus Span is independent evidence, not proof that Header was consumed.
	checkCompositionSpan(t, doc, doc.Children[0])
	if doc.HTML != "Only the body renders" {
		t.Fatalf("recording changed the constructor's actual field selection: %q", doc.HTML)
	}
}
