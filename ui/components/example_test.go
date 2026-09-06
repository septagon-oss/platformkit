package components_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"strings"
	"testing"

	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

var exampleInfo = c.ExampleInfo{ID: "button/default", ComponentID: "button", Group: "Action", Name: "Button"}

func describeExample(t *testing.T, example c.Example) c.ExampleDescription {
	t.Helper()
	description, err := example.Describe()
	if err != nil {
		t.Fatal(err)
	}
	return description
}

func TestExampleDescribesActualPropsAndPreservesTransport(t *testing.T) {
	example := c.ExampleOf(exampleInfo, c.ButtonProps{
		ComponentProps: c.ComponentProps{ID: "trusted", Class: "extra", Attrs: map[string]string{"data-local": "yes"}},
		HTMXProps:      c.HTMXProps{Post: "/save"},
		Label:          "Save",
	}, c.Button)
	description := describeExample(t, example)
	if !description.PropsEditable || description.ID != exampleInfo.ID || description.ComponentID != "button" || description.Group != "Action" || description.Name != "Button" {
		t.Fatalf("identity/support lost: %+v", description)
	}
	var props map[string]any
	var schema map[string]any
	if err := json.Unmarshal(description.Props, &props); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(description.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	for _, name := range []string{"id", "class", "attrs", "hx-post", "ComponentProps", "HTMXProps"} {
		if _, ok := props[name]; ok {
			t.Errorf("internal prop %q exported", name)
		}
		if _, ok := properties[name]; ok {
			t.Errorf("internal schema %q exported", name)
		}
	}
	if props["label"] != "Save" || schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" || schema["additionalProperties"] != false {
		t.Fatalf("unexpected props/schema: %s / %s", description.Props, description.Schema)
	}
	if properties["loading"].(map[string]any)["type"] != "boolean" || properties["label"].(map[string]any)["type"] != "string" {
		t.Fatal("schema does not describe actual Go field types")
	}
	updated, err := example.WithProps(json.RawMessage(`{"label":"Changed"}`))
	if err != nil {
		t.Fatal(err)
	}
	html := describeExample(t, updated).HTML
	for _, want := range []string{`id="trusted"`, `hx-post="/save"`, `data-local="yes"`, "extra", "Changed"} {
		if !strings.Contains(html, want) {
			t.Errorf("trusted transport or patch missing %q: %s", want, html)
		}
	}
	if strings.Contains(describeExample(t, example).HTML, "Changed") {
		t.Fatal("patch mutated original")
	}
}

func TestButtonTextRegionNamesItsActualProperty(t *testing.T) {
	for _, label := range []struct{ name, value, escaped string }{
		{"empty", "", ""},
		{"metacharacters", `Save & <tag>"'<!--/pk-text:label-->`, `Save &amp; &lt;tag&gt;&#34;&#39;&lt;!--/pk-text:label--&gt;`},
	} {
		t.Run(label.name, func(t *testing.T) {
			props := c.ButtonProps{
				ComponentProps: c.ComponentProps{Disabled: true, Attrs: map[string]string{"data-local": "kept"}},
				HTMXProps:      c.HTMXProps{Post: "/save", Target: "#result"},
				Label:          label.value, Loading: true,
			}
			before, err := json.Marshal(props)
			if err != nil {
				t.Fatal(err)
			}
			description := describeExample(t, c.ExampleOf(exampleInfo, props, c.Button))
			var values map[string]any
			var schema struct {
				Properties map[string]struct{ Type string }
			}
			if err := json.Unmarshal(description.Props, &values); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(description.Schema, &schema); err != nil {
				t.Fatal(err)
			}
			if values["label"] != label.value || schema.Properties["label"].Type != "string" {
				t.Fatal("the label region does not name the exported string property")
			}
			for _, marker := range []string{"<!--pk-text:label-->", "<!--/pk-text:label-->"} {
				if strings.Count(description.HTML, marker) != 1 {
					t.Fatalf("expected one exact %q marker: %s", marker, description.HTML)
				}
			}
			if !strings.Contains(description.HTML, "<!--pk-text:label-->"+label.escaped+"<!--/pk-text:label-->") {
				t.Fatal("the text region added a wrapper or failed to escape its label")
			}
			for _, attribute := range []string{`disabled`, `aria-busy="true"`, `hx-post="/save"`, `hx-target="#result"`, `data-local="kept"`} {
				if !strings.Contains(description.HTML, attribute) {
					t.Errorf("text annotation lost runtime attribute %s", attribute)
				}
			}
			after, err := json.Marshal(props)
			if err != nil || string(before) != string(after) {
				t.Fatalf("rendering changed caller-owned props: %s / %s (%v)", before, after, err)
			}
		})
	}
}

func TestButtonReplacedLabelHasNoTextRegion(t *testing.T) {
	original := c.ExampleWithSlots(exampleInfo, c.ButtonProps{Label: "Save"}, c.ButtonSlots{}, c.ButtonWithSlots)
	iconOnly, err := original.WithProps(json.RawMessage(`{"iconOnly":true}`))
	if err != nil {
		t.Fatal(err)
	}
	content, err := original.WithSlot("Content", g.Text("Save"))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []c.Example{iconOnly, content} {
		if strings.Contains(describeExample(t, example).HTML, "pk-text:label") {
			t.Fatal("an icon-only label or identical slot text was advertised as rendered label text")
		}
	}
	if !strings.Contains(describeExample(t, original).HTML, "<!--pk-text:label-->Save<!--/pk-text:label-->") {
		t.Fatal("replacement changed the original label region")
	}
}

func TestExampleStrictPatch(t *testing.T) {
	example := c.ExampleOf(exampleInfo, c.ButtonProps{Label: "Save"}, c.Button)
	for _, patch := range []string{
		`null`, `[]`, `true`, `{"Label":"bad"}`, `{"unknown":1}`, `{"id":"bad"}`,
		`{"class":"bad"}`, `{"attrs":{}}`, `{"hx-post":"bad"}`, `{"loading":null}`,
		`{"label":null}`, `{"label":3}`, `{"loading":"false"}`, `{"label":"x"} {}`,
		`{"label":"first","label":"second"}`,
	} {
		t.Run(patch, func(t *testing.T) {
			if _, err := example.WithProps(json.RawMessage(patch)); err == nil {
				t.Fatalf("accepted invalid patch %s", patch)
			}
		})
	}
}

func TestExampleNestedCopyAndReplacement(t *testing.T) {
	type child struct {
		Name   string           `json:"name"`
		Values map[string][]int `json:"values,omitempty"`
		Secret string           `json:"secret,omitempty" delivery:"internal"`
	}
	type props struct {
		Child   child          `json:"child"`
		Enabled *bool          `json:"enabled,omitempty"`
		Any     map[string]any `json:"any,omitempty"`
	}
	input := props{Child: child{Name: "first", Values: map[string][]int{"n": {1, 2}}, Secret: "private"}, Enabled: new(false), Any: map[string]any{"nested": map[string]any{"n": []int{4}}}}
	example := c.ExampleOf(exampleInfo, input, func(p props) g.Node {
		data, _ := json.Marshal(p)
		p.Child.Name = "renderer mutation"
		p.Any["changed"] = true
		return g.Text(string(data))
	})
	before := describeExample(t, example)
	input.Child.Values["n"][0] = 9
	input.Any["nested"].(map[string]any)["n"].([]int)[0] = 8
	*input.Enabled = true
	if after := describeExample(t, example); string(after.Props) != string(before.Props) || after.HTML != before.HTML {
		t.Fatal("captured props changed through input or render aliases")
	}
	if !strings.Contains(string(before.Props), `"enabled":false`) || strings.Contains(string(before.Props), "private") {
		t.Fatalf("false pointer or internal filtering lost: %s", before.Props)
	}
	updated, err := example.WithProps(json.RawMessage(`{"child":{"name":"new"},"enabled":false}`))
	if err != nil {
		t.Fatal(err)
	}
	after := describeExample(t, updated)
	if strings.Contains(string(after.Props), `"values"`) || !strings.Contains(string(after.Props), `"name":"new"`) || !strings.Contains(string(after.Props), `"enabled":false`) {
		t.Fatalf("nested patch merged instead of replacing: %s", after.Props)
	}
	for _, patch := range []string{`{"child":{"Name":"bad"}}`, `{"child":{"secret":"bad"}}`, `{"child":{"values":{"n":[null]}}}`, `{"child":null}`} {
		if _, err := example.WithProps(json.RawMessage(patch)); err == nil {
			t.Errorf("accepted nested invalid patch %s", patch)
		}
	}
}

func TestExampleSlotReplacementCopiesContainers(t *testing.T) {
	icons := []g.Node{g.Text("old-icon")}
	button := c.ExampleWithSlots(exampleInfo, c.ButtonProps{Label: "Save"}, c.ButtonSlots{IconEnd: icons}, c.ButtonWithSlots)
	icons[0] = g.Text("caller-mutation")
	replacement := []g.Node{g.Text("new-icon")}
	updated, err := button.WithSlot("IconEnd", replacement...)
	if err != nil {
		t.Fatal(err)
	}
	replacement[0] = g.Text("replacement-mutation")
	originalHTML, updatedHTML := describeExample(t, button).HTML, describeExample(t, updated).HTML
	if !strings.Contains(originalHTML, "old-icon") || strings.Contains(originalHTML, "new-icon") || !strings.Contains(updatedHTML, "new-icon") || strings.Contains(updatedHTML, "mutation") {
		t.Fatalf("slot alias: %s / %s", originalHTML, updatedHTML)
	}
	if strings.Index(updatedHTML, "Save") > strings.Index(updatedHTML, "new-icon") {
		t.Fatal("replacement did not use canonical trailing icon renderer")
	}
	if _, err := button.WithSlot("iconEnd", g.Text("wrong-case")); err == nil {
		t.Fatal("accepted slot case folding")
	}
	card := c.ExampleWithSlots(exampleInfo, c.CardProps{}, c.CardSlots{Header: []g.Node{g.Text("old-header")}, Content: []g.Node{g.Text("body")}}, c.CardWithSlots)
	newCard, err := card.WithSlot("Header", g.Text("new-header"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(describeExample(t, card).HTML, "old-header") || !strings.Contains(describeExample(t, newCard).HTML, "new-header") || !strings.Contains(describeExample(t, newCard).HTML, "body") {
		t.Fatal("card replacement mutated unrelated content")
	}
	for _, slot := range describeExample(t, button).Slots {
		if !slot.Supported || !slot.Multiple || !slot.TrustedOnly || slot.GoType != "[]gomponents.Node" {
			t.Fatalf("inaccurate slot contract: %+v", slot)
		}
	}
}

func TestExampleChildrenAndUnsupportedSlots(t *testing.T) {
	children := c.ExampleWithChildren(exampleInfo, c.CardProps{}, []g.Node{g.Text("before")}, c.Card)
	changed, err := children.WithSlot("children", g.Text("after"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(describeExample(t, changed).HTML, "after") || describeExample(t, children).Slots[0].Name != "children" {
		t.Fatal("children contract missing")
	}
	table := c.ExampleWithSlots(exampleInfo, c.TableProps{}, c.TableSlots{}, c.TableWithSlots)
	for _, slot := range describeExample(t, table).Slots {
		if slot.Supported {
			t.Errorf("callback/config slot advertised replaceable: %+v", slot)
		}
		if _, err := table.WithSlot(slot.Name, g.Text("bad")); err == nil {
			t.Errorf("accepted non-node slot %s", slot.Name)
		}
	}
	type slots struct {
		One      g.Node `json:"one"`
		Compound []struct{ Child g.Node }
	}
	one := c.ExampleWithSlots(exampleInfo, c.CardProps{}, slots{One: g.Text("one")}, func(p c.CardProps, s slots) g.Node { return c.Card(p, s.One) })
	if _, err := one.WithSlot("one", g.Text("a"), g.Text("b")); err == nil {
		t.Fatal("accepted multiple nodes for single-node slot")
	}
	if _, err := one.WithSlot("Compound", g.Text("bad")); err == nil {
		t.Fatal("accepted compound slot")
	}
	if cleared, err := one.WithSlot("one"); err != nil || strings.Contains(describeExample(t, cleared).HTML, ">one<") {
		t.Fatal("single node cannot be cleared")
	}
}

func TestExamplePreviewAndRenderFailure(t *testing.T) {
	preview := c.ExamplePreview(exampleInfo, g.Text("preview"), "helper has no Props contract")
	description := describeExample(t, preview)
	if description.PropsEditable || description.Reason == "" || description.HTML != "preview" {
		t.Fatalf("preview masquerades as editable: %+v", description)
	}
	if _, err := preview.WithProps(json.RawMessage(`{}`)); err == nil {
		t.Fatal("preview accepted props")
	}
	if _, err := preview.WithSlot("children", g.Text("bad")); err == nil {
		t.Fatal("preview accepted slot")
	}
	failure := errors.New("render failed")
	broken := c.ExampleOf(exampleInfo, c.ButtonProps{}, func(c.ButtonProps) g.Node {
		return g.NodeFunc(func(io.Writer) error { return failure })
	})
	if _, err := broken.Describe(); !errors.Is(err, failure) {
		t.Fatalf("render failure hidden: %v", err)
	}
}

func TestEveryGalleryExampleHasAnAccurateDescription(t *testing.T) {
	for _, example := range c.Gallery() {
		t.Run(example.ID, func(t *testing.T) {
			description := describeExample(t, example)
			if description.HTML == "" || (!description.PropsEditable && description.Reason == "") {
				t.Fatalf("incomplete description: %+v", description)
			}
			if description.PropsEditable {
				reapplied, err := example.WithProps(description.Props)
				if err != nil {
					t.Fatalf("exported properties cannot be reapplied: %v", err)
				}
				if describeExample(t, reapplied).HTML != description.HTML {
					t.Fatal("reapplying exported properties changed the rendering")
				}
			}
		})
	}
	sidebar := c.ExampleOf(exampleInfo, c.SidebarProps{Items: []c.SidebarItem{{Label: "Parent", Children: []c.SidebarItem{{Label: "Child"}}}}}, c.Sidebar)
	if schema := string(describeExample(t, sidebar).Schema); !strings.Contains(schema, `"$defs"`) || !strings.Contains(schema, `"$ref"`) {
		t.Fatalf("recursive SidebarItem data not represented: %s", schema)
	}
}

func TestExampleNodesRemainOpaqueTrustedReferences(t *testing.T) {
	// We copy slot containers, not arbitrary state captured by a Go Node.
	text := "before"
	node := g.NodeFunc(func(w io.Writer) error { _, err := io.WriteString(w, text); return err })
	example := c.ExampleWithChildren(exampleInfo, c.CardProps{}, []g.Node{node}, c.Card)
	text = "after"
	if !strings.Contains(describeExample(t, example).HTML, "after") {
		t.Fatal("opaque Node capability unexpectedly replaced")
	}
}

func TestExampleTablePatchKeepsJSONNumberPrecision(t *testing.T) {
	example := c.ExampleOf(exampleInfo, c.TableProps{}, c.Table)
	updated, err := example.WithProps(json.RawMessage(`{"rows":[{"cells":{"n":9007199254740993}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if props := string(describeExample(t, updated).Props); !strings.Contains(props, `9007199254740993`) {
		t.Fatalf("untyped cell number lost precision: %s", props)
	}
}

type opaqueExampleNode struct{ Label string }

func (n opaqueExampleNode) Render(w io.Writer) error {
	_, err := io.WriteString(w, n.Label)
	return err
}

type encodedExampleValue struct{ Label string }

func (v encodedExampleValue) MarshalJSON() ([]byte, error) { return json.Marshal(v.Label) }

func TestExampleRefusesOpaqueValuesHiddenInsideAny(t *testing.T) {
	type props struct {
		Value any `json:"value"`
	}
	for _, value := range []any{opaqueExampleNode{Label: "node"}, encodedExampleValue{Label: "custom codec"}, netip.MustParseAddr("192.0.2.1")} {
		example := c.ExampleOf(exampleInfo, props{Value: value}, func(props) g.Node { return g.Text("Preview") })
		if _, err := example.Describe(); err == nil {
			t.Errorf("opaque dynamic value %T was silently projected as a data struct", value)
		}
	}
}

func TestExampleRejectsCyclicDataWithoutMutatingIt(t *testing.T) {
	data := map[string]any{}
	data["self"] = data
	example := c.ExampleOf(exampleInfo, struct{ Data map[string]any }{Data: data}, func(struct{ Data map[string]any }) g.Node { return g.Text("Cycle") })
	if _, err := example.Describe(); err == nil {
		t.Fatal("cyclic JSON data was accepted")
	}
	if len(data) != 1 {
		t.Fatal("cycle detection changed caller data")
	}
}

func TestExampleTypedJSONNumberHasNumericSchema(t *testing.T) {
	type props struct {
		N json.Number `json:"n"`
	}
	example := c.ExampleOf(exampleInfo, props{N: "9007199254740993"}, func(p props) g.Node { return g.Text(p.N.String()) })
	description := describeExample(t, example)
	var schema struct {
		Properties map[string]struct{ Type string }
	}
	if err := json.Unmarshal(description.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties["n"].Type != "number" || string(description.Props) != `{"n":9007199254740993}` {
		t.Fatalf("number value/schema disagree: %s / %s", description.Props, description.Schema)
	}
	updated, err := example.WithProps(json.RawMessage(`{"n":9007199254740995}`))
	if err != nil || describeExample(t, updated).HTML != "9007199254740995" {
		t.Fatalf("typed JSON number patch lost precision: %v", err)
	}
	for _, patch := range []string{`{"n":"123"}`, `{"n":null}`, `{"n":true}`} {
		if _, err := example.WithProps(json.RawMessage(patch)); err == nil {
			t.Errorf("non-numeric JSON accepted for number: %s", patch)
		}
	}
}

func TestExampleNilEmbeddedPointerDoesNotRequireAbsentFields(t *testing.T) {
	type EmbeddedProps struct {
		Label string `json:"label"`
	}
	type props struct{ *EmbeddedProps }
	example := c.ExampleOf(exampleInfo, props{}, func(p props) g.Node {
		if p.EmbeddedProps == nil {
			return g.Text("Absent")
		}
		return g.Text(p.Label)
	})
	description := describeExample(t, example)
	var schema struct{ Required []string }
	if err := json.Unmarshal(description.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 0 || string(description.Props) != `{}` {
		t.Fatalf("nil promoted field disagrees with schema: %s / %s", description.Props, description.Schema)
	}
	updated, err := example.WithProps(json.RawMessage(`{"label":"Present"}`))
	if err != nil || describeExample(t, updated).HTML != "Present" || describeExample(t, example).HTML != "Absent" {
		t.Fatalf("promoted field patch did not isolate pointer allocation: %v", err)
	}
}
