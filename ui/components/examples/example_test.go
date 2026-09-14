package examples_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

var exampleInfo = examples.ExampleInfo{ID: "button/default", ComponentID: "button", Group: "Action", Name: "Button"}

func describeExample(t *testing.T, example examples.Example) examples.ExampleDescription {
	t.Helper()
	description, err := example.Describe()
	if err != nil {
		t.Fatal(err)
	}
	return description
}

func TestExampleDescribesActualPropsAndPreservesTransport(t *testing.T) {
	example := examples.ExampleOf(exampleInfo, c.ButtonProps{
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
			description := describeExample(t, examples.ExampleOf(exampleInfo, props, c.Button))
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

func TestAlertTextRegionsPreservePropertiesAndAnnouncements(t *testing.T) {
	t.Parallel()
	for _, tone := range []string{"info", "danger"} {
		for _, value := range []string{"", `Review & <tag>"'<!--/pk-text:message-->`} {
			props := c.AlertProps{Title: value, Message: value, Tone: tone, Dismissible: true, Bordered: true}
			example := examples.ExampleWithSlots(exampleInfo, props, c.AlertSlots{Actions: []g.Node{g.Text("Trusted action")}}, c.AlertWithSlots)
			description := describeExample(t, example)
			var escaped strings.Builder
			if err := g.Text(value).Render(&escaped); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"title", "message"} {
				open, close := "<!--pk-text:"+field+"-->", "<!--/pk-text:"+field+"-->"
				if field == "title" && value == "" {
					if strings.Contains(description.HTML, open) {
						t.Fatal("absent title advertised an editable text region")
					}
				} else if strings.Count(description.HTML, open) != 1 || !strings.Contains(description.HTML, open+escaped.String()+close) {
					t.Fatalf("%s text region lost property identity or escaping: %s", field, description.HTML)
				}
			}
			role, live := "status", "polite"
			if tone == "danger" {
				role, live = "alert", "assertive"
			}
			for _, want := range []string{`role="` + role + `"`, `aria-live="` + live + `"`, `aria-atomic="true"`, `aria-label="Dismiss notification"`, "Trusted action"} {
				if !strings.Contains(description.HTML, want) {
					t.Fatalf("annotation changed runtime semantics: missing %s", want)
				}
			}
			changed, err := example.WithProps(json.RawMessage(`{"message":"Changed"}`))
			if err != nil || !strings.Contains(describeExample(t, changed).HTML, "<!--pk-text:message-->Changed<!--/pk-text:message-->") {
				t.Fatalf("message cannot be projected through its existing property: %v", err)
			}
			if describeExample(t, example).HTML != description.HTML {
				t.Fatal("projection changed caller-owned source")
			}
		}
	}
}

func TestButtonReplacedLabelHasNoTextRegion(t *testing.T) {
	original := examples.ExampleWithSlots(exampleInfo, c.ButtonProps{Label: "Save"}, c.ButtonSlots{}, c.ButtonWithSlots)
	iconOnly, err := original.WithProps(json.RawMessage(`{"iconOnly":true}`))
	if err != nil {
		t.Fatal(err)
	}
	content, err := original.WithSlot("Content", g.Text("Save"))
	if err != nil {
		t.Fatal(err)
	}
	for _, example := range []examples.Example{iconOnly, content} {
		if strings.Contains(describeExample(t, example).HTML, "pk-text:label") {
			t.Fatal("an icon-only label or identical slot text was advertised as rendered label text")
		}
	}
	if !strings.Contains(describeExample(t, original).HTML, "<!--pk-text:label-->Save<!--/pk-text:label-->") {
		t.Fatal("replacement changed the original label region")
	}
}

func TestInputAndLabelTextRegionsPreserveNativeSemantics(t *testing.T) {
	for _, tc := range []struct{ name, value, escaped string }{
		{"empty", "", ""},
		{"escaped", `Title & <tag>"'<!--/pk-text:label-->`, `Title &amp; &lt;tag&gt;&#34;&#39;&lt;!--/pk-text:label--&gt;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			props := c.InputProps{
				ComponentProps: c.ComponentProps{ID: "title-field", Attrs: map[string]string{"data-local": "kept"}},
				HTMXProps:      c.HTMXProps{Post: "/validate", Target: "#result"},
				Name:           "title", Label: tc.value, Value: tc.value, Required: true,
				Error: "Check this title", HelpText: "Public title", Autocomplete: "off",
			}
			before, err := json.Marshal(props)
			if err != nil {
				t.Fatal(err)
			}
			description := describeExample(t, examples.ExampleOf(exampleInfo, props, c.Input))
			if tc.value == "" {
				if strings.Contains(description.HTML, "pk-text:label") || strings.Contains(description.HTML, "<label") {
					t.Fatal("absent Input label was advertised as rendered text")
				}
			} else {
				region := "<!--pk-text:label-->" + tc.escaped + "<!--/pk-text:label-->"
				if strings.Count(description.HTML, "<!--pk-text:label-->") != 1 || !strings.Contains(description.HTML, region+"<span") {
					t.Fatalf("Input label must own exactly its escaped text, excluding required marker: %s", description.HTML)
				}
				if strings.Contains(description.HTML, "pk-text:text") || !strings.Contains(description.HTML, `for="title-field"`) {
					t.Fatal("Input label lost source-property ownership or native association")
				}
			}
			for _, attribute := range []string{`id="title-field"`, `name="title"`, `required`, `aria-invalid="true"`, `aria-describedby="title-field-error title-field-help"`, `hx-post="/validate"`, `hx-target="#result"`, `autocomplete="off"`, `data-local="kept"`, `border-solid`} {
				if !strings.Contains(description.HTML, attribute) {
					t.Errorf("Input annotation lost %s", attribute)
				}
			}
			label := describeExample(t, examples.ExampleOf(exampleInfo, c.LabelProps{Text: tc.value, For: "title-field", Required: true}, c.Label))
			if !strings.Contains(label.HTML, "<!--pk-text:text-->"+tc.escaped+"<!--/pk-text:text--><span") || !strings.Contains(label.HTML, `aria-hidden="true"> *</span>`) {
				t.Fatalf("Label property region changed text or required semantics: %s", label.HTML)
			}
			after, err := json.Marshal(props)
			if err != nil || string(before) != string(after) {
				t.Fatalf("rendering mutated caller props: %v", err)
			}
		})
	}
}

func TestTextContentRegionPreservesSemanticElementAndEscaping(t *testing.T) {
	for _, value := range []string{"", "Album description", `A & <tag>"'<!--/pk-text:content-->`} {
		for _, element := range []string{"p", "div", "strong", "h1"} {
			props := c.TextProps{Content: value, Element: element}
			description := describeExample(t, examples.ExampleOf(exampleInfo, props, c.Text))
			var escaped strings.Builder
			if err := g.Text(value).Render(&escaped); err != nil {
				t.Fatal(err)
			}
			region := "<!--pk-text:content-->" + escaped.String() + "<!--/pk-text:content-->"
			if strings.Count(description.HTML, "<!--pk-text:content-->") != 1 || !strings.Contains(description.HTML, region) {
				t.Fatalf("Text must bind exactly its escaped content: %s", description.HTML)
			}
			wantElement := element
			if element == "h1" {
				wantElement = "p"
			}
			if !strings.HasPrefix(description.HTML, "<"+wantElement+" ") || !strings.HasSuffix(description.HTML, "</"+wantElement+">") {
				t.Fatalf("Text annotation changed semantic element: %s", description.HTML)
			}
		}
	}
}

func TestToolbarTextRegionsBelongToToolbarProps(t *testing.T) {
	for _, value := range []string{"", "Act", `A & <tag>"'<!--/pk-text:Title-->`} {
		action := examples.ExampleOf(examples.ExampleInfo{ID: "action", ComponentID: "button"}, c.ButtonProps{Label: "Act"}, c.Button)
		original := examples.ExampleWithChildren(examples.ExampleInfo{ID: "toolbar", ComponentID: "toolbar"},
			c.ToolbarProps{Title: value, Subtitle: value}, []g.Node{action.Node}, c.Toolbar)
		description := describeExample(t, original)
		var escaped strings.Builder
		if err := g.Text(value).Render(&escaped); err != nil {
			t.Fatal(err)
		}
		want := 1
		if value == "" {
			want = 0
		}
		for _, field := range []string{"Title", "Subtitle"} {
			open, close := "<!--pk-text:"+field+"-->", "<!--/pk-text:"+field+"-->"
			if strings.Count(description.HTML, open) != want || strings.Count(description.HTML, close) != want ||
				(want == 1 && !strings.Contains(description.HTML, open+escaped.String()+close)) {
				t.Fatalf("Toolbar must own exactly its rendered %s: %s", field, description.HTML)
			}
		}
		if strings.Contains(description.HTML, "<!--pk-text:content-->") || strings.Contains(description.HTML, "<!--pk-text:text-->") ||
			strings.Contains(description.HTML, `data-component="text"`) ||
			strings.Count(description.HTML, "<h1 ") != want || strings.Count(description.HTML, "<p ") != want {
			t.Fatalf("Toolbar copy lost its native elements or borrowed an atom's identity or property: %s", description.HTML)
		}
		if len(description.Children) != 1 || description.Children[0].Description.ID != "action" || description.Children[0].Span == nil {
			t.Fatal("Toolbar annotation lost the observed action occurrence")
		}
		updated, err := original.WithProps(json.RawMessage(`{"Title":"Changed","Subtitle":"Revised"}`))
		if err != nil {
			t.Fatal(err)
		}
		changed := describeExample(t, updated)
		if !strings.Contains(changed.HTML, "<!--pk-text:Title-->Changed<!--/pk-text:Title-->") ||
			!strings.Contains(changed.HTML, "<!--pk-text:Subtitle-->Revised<!--/pk-text:Subtitle-->") ||
			changed.Children[0].Description.HTML != description.Children[0].Description.HTML || describeExample(t, original).HTML != description.HTML {
			t.Fatal("Toolbar projection lost property ownership or changed the action or original")
		}
	}
}

func TestCardTextRegionsFollowRenderedHeaderOwnership(t *testing.T) {
	for _, tc := range []struct {
		name  string
		slots c.CardSlots
		count int
	}{
		{name: "plain", count: 1},
		{name: "sectioned fallback", slots: c.CardSlots{Content: []g.Node{g.Text("Body")}}, count: 1},
		{name: "caller header", slots: c.CardSlots{Header: []g.Node{g.Text("Custom")}}, count: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			example := examples.ExampleWithSlots(examples.ExampleInfo{ID: "card", ComponentID: "card"},
				c.CardProps{Title: "A & <title>", Description: "A & <description>"}, tc.slots, c.CardWithSlots)
			description := describeExample(t, example)
			for _, field := range []string{"title", "description"} {
				open, close := "<!--pk-text:"+field+"-->", "<!--/pk-text:"+field+"-->"
				if strings.Count(description.HTML, open) != tc.count || strings.Count(description.HTML, close) != tc.count ||
					(tc.count == 1 && !strings.Contains(description.HTML, open+"A &amp; &lt;"+field+"&gt;"+close)) {
					t.Fatalf("Card must bind only its rendered %s: %s", field, description.HTML)
				}
			}
			if strings.Contains(description.HTML, "pk-text:content") || strings.Contains(description.HTML, "pk-text:text") ||
				strings.Count(description.HTML, "<p ") != tc.count*2 {
				t.Fatalf("Card copy changed semantic elements or borrowed another contract: %s", description.HTML)
			}
		})
	}
	empty := examples.ExampleWithSlots(examples.ExampleInfo{ID: "empty", ComponentID: "card"}, c.CardProps{}, c.CardSlots{}, c.CardWithSlots)
	if strings.Contains(describeExample(t, empty).HTML, "pk-text:") {
		t.Fatal("Absent Card copy must not invent text regions")
	}
}

func TestEmptyStateTextRegionsRetainOwnershipAndEscaping(t *testing.T) {
	for _, value := range []string{"", "Gather memories", `A & <tag>"'<!--/pk-text:description-->`} {
		example := examples.ExampleWithSlots(examples.ExampleInfo{ID: "empty", ComponentID: "empty-state"},
			c.EmptyStateProps{Title: value, Description: value}, c.EmptyStateSlots{Actions: []g.Node{
				examples.ExampleOf(examples.ExampleInfo{ID: "action", ComponentID: "button"}, c.ButtonProps{Label: "Create"}, c.Button).Node,
			}}, c.EmptyStateWithSlots)
		description := describeExample(t, example)
		var escaped strings.Builder
		if err := g.Text(value).Render(&escaped); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"title", "description"} {
			open, close := "<!--pk-text:"+field+"-->", "<!--/pk-text:"+field+"-->"
			if field == "description" && value == "" {
				if strings.Contains(description.HTML, open) {
					t.Fatal("Absent description must not invent a text region")
				}
			} else if strings.Count(description.HTML, open) != 1 || !strings.Contains(description.HTML, open+escaped.String()+close) {
				t.Fatalf("EmptyState must bind only its escaped %s: %s", field, description.HTML)
			}
		}
		changed, err := example.WithProps(json.RawMessage(`{"title":"Revised","description":"Add memories"}`))
		if err != nil {
			t.Fatal(err)
		}
		revised := describeExample(t, changed)
		if !strings.Contains(revised.HTML, "<!--pk-text:title-->Revised<!--/pk-text:title-->") ||
			!strings.Contains(revised.HTML, "<!--pk-text:description-->Add memories<!--/pk-text:description-->") ||
			len(revised.Children) != 1 || revised.Children[0].Description.ID != "action" ||
			describeExample(t, example).HTML != description.HTML {
			t.Fatal("EmptyState edits must preserve linked actions and the original invocation")
		}
	}
}

func TestInputValueRegionIsLimitedToTextControls(t *testing.T) {
	for _, typ := range []string{"", "text", " TEXT ", "email", "password", "number", "tel", "url", "search", "date", "time", "datetime-local", "month", "week", "color", "hidden", "file"} {
		t.Run(typ, func(t *testing.T) {
			for _, value := range []string{"", `A & <tag>"'`} {
				description := describeExample(t, examples.ExampleOf(exampleInfo, c.InputProps{Name: "title", Type: typ, Value: value}, c.Input))
				want := typ == "" || strings.EqualFold(strings.TrimSpace(typ), "text")
				if count := strings.Count(description.HTML, `data-pk-value="value"`); count != 0 && !want || count != 1 && want {
					t.Fatalf("type %q value %q has %d value markers", typ, value, count)
				}
				if value != "" && typ != "file" && !strings.Contains(description.HTML, `value="A &amp; &lt;tag&gt;&#34;&#39;"`) {
					t.Fatal("annotation changed native value escaping")
				}
				if typ == "file" && strings.Contains(description.HTML, `value=`) {
					t.Fatal("file input carries a value")
				}
			}
		})
	}
	original := examples.ExampleOf(exampleInfo, c.InputProps{Name: "title"}, c.Input)
	filled, err := original.WithProps(json.RawMessage(`{"value":"New title"}`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, err := filled.WithProps(json.RawMessage(`{"value":""}`))
	if err != nil {
		t.Fatal(err)
	}
	initial := describeExample(t, original)
	if !strings.Contains(describeExample(t, filled).HTML, `value="New title"`) || string(initial.Props) != `{"name":"title"}` || describeExample(t, cleared).HTML != initial.HTML {
		t.Fatal("setting and clearing a value changed omission semantics or original input")
	}
}

func TestInputFamilyDeclaresSolidBorders(t *testing.T) {
	for _, node := range []g.Node{
		c.Input(c.InputProps{Name: "field", Required: true}),
		c.Select(c.SelectProps{Name: "field", Required: true}),
		c.Textarea(c.TextareaProps{Name: "field", Required: true}),
	} {
		var html strings.Builder
		if err := node.Render(&html); err != nil {
			t.Fatal(err)
		}
		for _, attribute := range []string{`border-solid`, `name="field"`, `required`} {
			if !strings.Contains(html.String(), attribute) {
				t.Errorf("input-family control lost %s: %s", attribute, html.String())
			}
		}
	}
}

func TestTextareaRegionsRetainOwningCopyAndLeadingNewlines(t *testing.T) {
	for _, tc := range []struct{ value, escaped string }{
		{"", ""}, {"A & <tag>", "A &amp; &lt;tag&gt;"}, {"\nFirst\nLast\n", "\n\nFirst\nLast\n"},
		{"\r\nFirst\r\n", "\n\r\nFirst\r\n"},
	} {
		example := examples.ExampleOf(exampleInfo, c.TextareaProps{Name: "description", Label: "A & <label>", Value: tc.value, Rows: 5, Required: true}, c.Textarea)
		description := describeExample(t, example)
		if strings.Count(description.HTML, `data-pk-value="value"`) != 1 ||
			!strings.Contains(description.HTML, "<!--pk-text:label-->A &amp; &lt;label&gt;<!--/pk-text:label-->") ||
			strings.Contains(description.HTML, "pk-text:text") {
			t.Fatalf("Textarea must own label and native value regions: %s", description.HTML)
		}
		if !strings.Contains(description.HTML, ">"+tc.escaped+"</textarea>") ||
			!strings.Contains(description.HTML, `for="pk-textarea-description"`) || !strings.Contains(description.HTML, `rows="5"`) {
			t.Fatalf("Textarea changed escaped content, initial newline or label semantics: %s", description.HTML)
		}
	}
}

func TestExampleStringDefaultsDescribeOnlyDefiniteOmittedZeros(t *testing.T) {
	type namedString string
	type EmbeddedStrings struct {
		Promoted string `json:"promoted,omitempty"`
	}
	type OptionalStrings struct {
		Optional string `json:"optional,omitempty"`
	}
	type props struct {
		EmbeddedStrings
		*OptionalStrings
		Empty    string      `json:"empty,omitempty"`
		Zero     string      `json:"zero,omitzero"`
		Named    namedString `json:"named,omitempty"`
		Required string      `json:"required"`
		Pointer  *string     `json:"pointer,omitempty"`
		Number   json.Number `json:"number,omitempty"`
	}
	original := examples.ExampleOf(exampleInfo, props{Number: "1"}, func(props) g.Node { return g.Text("unchanged") })
	description := describeExample(t, original)
	var schema struct{ Properties map[string]map[string]any }
	if err := json.Unmarshal(description.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"empty", "zero", "named", "promoted"} {
		property := schema.Properties[name]
		if value, ok := property["default"]; !ok || value != "" || property["type"] != "string" {
			t.Errorf("%s lacks its concrete omitted string zero: %+v", name, property)
		}
	}
	for _, name := range []string{"required", "pointer", "optional", "number"} {
		if _, ok := schema.Properties[name]["default"]; ok {
			t.Errorf("%s advertises an unsupported default", name)
		}
	}
	if string(description.Props) != `{"number":1,"required":""}` {
		t.Fatalf("schema defaults materialized absent props: %s", description.Props)
	}
	updated, err := original.WithProps(json.RawMessage(`{"empty":"filled","optional":"present","pointer":"set"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(describeExample(t, updated).Schema) != string(description.Schema) || string(describeExample(t, original).Props) != string(description.Props) {
		t.Fatal("defaults depend on instance values or mutated the original")
	}
	input := describeExample(t, examples.ExampleOf(exampleInfo, c.InputProps{Name: "title"}, c.Input))
	if err := json.Unmarshal(input.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Properties["value"]["default"] != "" || strings.Contains(string(input.Props), `"value"`) {
		t.Fatal("empty Input value needs schema evidence without changing props omission")
	}
}

func TestButtonSlotRegionsFollowTheRenderedComposition(t *testing.T) {
	for _, tc := range []struct {
		name    string
		props   c.ButtonProps
		slots   c.ButtonSlots
		regions []string
	}{
		{name: "absent"},
		{name: "leading", slots: c.ButtonSlots{IconStart: []g.Node{g.Text("Start &"), g.El("i")}},
			regions: []string{"<!--pk-slot:IconStart-->Start &amp;<i></i><!--/pk-slot:IconStart-->"}},
		{name: "trailing", slots: c.ButtonSlots{IconEnd: []g.Node{g.Text("End")}},
			regions: []string{"<!--pk-slot:IconEnd-->End<!--/pk-slot:IconEnd-->"}},
		{name: "empty rendered group", slots: c.ButtonSlots{IconStart: []g.Node{g.Group{}}},
			regions: []string{"<!--pk-slot:IconStart--><!--/pk-slot:IconStart-->"}},
		{name: "content owns its name", slots: c.ButtonSlots{
			Content: []g.Node{g.Text("Own")}, IconStart: []g.Node{g.Text("ignored")}, IconEnd: []g.Node{g.Text("ignored")}},
			regions: []string{"<!--pk-slot:Content-->Own<!--/pk-slot:Content-->"}},
		{name: "loading replaces icons", props: c.ButtonProps{Loading: true},
			slots: c.ButtonSlots{IconStart: []g.Node{g.Text("ignored")}, IconEnd: []g.Node{g.Text("ignored")}}},
		{name: "icon only", props: c.ButtonProps{IconOnly: true}, slots: c.ButtonSlots{
			IconStart: []g.Node{g.Text("First")}, IconEnd: []g.Node{g.Text("Last")}},
			regions: []string{"<!--pk-slot:IconStart-->First<!--/pk-slot:IconStart-->", "<!--pk-slot:IconEnd-->Last<!--/pk-slot:IconEnd-->"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.props.Label = "Save"
			example := examples.ExampleWithSlots(exampleInfo, tc.props, tc.slots, c.ButtonWithSlots)
			description := describeExample(t, example)
			if strings.Count(description.HTML, "<!--pk-slot:") != len(tc.regions) {
				t.Fatalf("rendered slot ownership differs: %s", description.HTML)
			}
			for _, region := range tc.regions {
				if !strings.Contains(description.HTML, region) {
					t.Errorf("missing exact unwrapped region %s", region)
				}
			}
			if strings.Contains(description.HTML, "ignored") {
				t.Fatal("a replaced slot was rendered")
			}
			if tc.props.IconOnly && !strings.Contains(description.HTML, `aria-label="Save"`) {
				t.Fatal("source regions changed the icon-only accessible name")
			}
			if describeExample(t, example).HTML != description.HTML {
				t.Fatal("rendering changed the source invocation")
			}
		})
	}
}

func TestButtonSlotRegionsCopyCallerContainers(t *testing.T) {
	for _, href := range []string{"", "/next"} {
		nodes := []g.Node{g.Text("before"), g.Attr("data-supplied", "yes")}
		for name, slots := range map[string]c.ButtonSlots{
			"IconStart": {IconStart: nodes}, "IconEnd": {IconEnd: nodes}, "Content": {Content: nodes},
		} {
			nodes[0] = g.Text("before")
			button := c.ButtonWithSlots(c.ButtonProps{Label: "Save", Href: href}, slots)
			nodes[0] = g.Text("after")
			var rendered strings.Builder
			if err := button.Render(&rendered); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(rendered.String(), "<!--pk-slot:"+name+"-->before<!--/pk-slot:"+name+"-->") ||
				!strings.Contains(rendered.String(), `data-supplied="yes"`) {
				t.Fatalf("slot %s changed caller-container or attribute semantics: %s", name, &rendered)
			}
		}
	}
}

func TestExampleStrictPatch(t *testing.T) {
	example := examples.ExampleOf(exampleInfo, c.ButtonProps{Label: "Save"}, c.Button)
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
	example := examples.ExampleOf(exampleInfo, input, func(p props) g.Node {
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
	button := examples.ExampleWithSlots(exampleInfo, c.ButtonProps{Label: "Save"}, c.ButtonSlots{IconEnd: icons}, c.ButtonWithSlots)
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
	if !strings.Contains(updatedHTML, "<!--pk-slot:IconEnd-->new-icon<!--/pk-slot:IconEnd-->") {
		t.Fatal("replacement lost its unwrapped source slot identity")
	}
	cleared, err := button.WithSlot("IconEnd")
	if err != nil || strings.Contains(describeExample(t, cleared).HTML, "<!--pk-slot:") {
		t.Fatal("cleared slot retained a rendered region")
	}
	if _, err := button.WithSlot("iconEnd", g.Text("wrong-case")); err == nil {
		t.Fatal("accepted slot case folding")
	}
	card := examples.ExampleWithSlots(exampleInfo, c.CardProps{}, c.CardSlots{Header: []g.Node{g.Text("old-header")}, Content: []g.Node{g.Text("body")}}, c.CardWithSlots)
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
	children := examples.ExampleWithChildren(exampleInfo, c.CardProps{}, []g.Node{g.Text("before")}, c.Card)
	changed, err := children.WithSlot("children", g.Text("after"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(describeExample(t, changed).HTML, "after") || describeExample(t, children).Slots[0].Name != "children" {
		t.Fatal("children contract missing")
	}
	table := examples.ExampleWithSlots(exampleInfo, c.TableProps{}, c.TableSlots{}, c.TableWithSlots)
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
	one := examples.ExampleWithSlots(exampleInfo, c.CardProps{}, slots{One: g.Text("one")}, func(p c.CardProps, s slots) g.Node { return c.Card(p, s.One) })
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
	preview := examples.ExamplePreview(exampleInfo, g.Text("preview"), "helper has no Props contract")
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
	broken := examples.ExampleOf(exampleInfo, c.ButtonProps{}, func(c.ButtonProps) g.Node {
		return g.NodeFunc(func(io.Writer) error { return failure })
	})
	if _, err := broken.Describe(); !errors.Is(err, failure) {
		t.Fatalf("render failure hidden: %v", err)
	}
}

func TestHeadingKeepsSemanticLevelAndEscapedSourceProperty(t *testing.T) {
	for level := range 6 {
		text := `An album & <memories>`
		example := examples.ExampleOf(examples.ExampleInfo{ID: "heading", ComponentID: "pk-ui.component.heading"},
			c.HeadingProps{Level: level + 1, Text: text, Anchor: "album"}, c.Heading)
		description := describeExample(t, example)
		want := `<!--pk-text:text-->An album &amp; &lt;memories&gt;<!--/pk-text:text-->`
		if !strings.Contains(description.HTML, want) || !strings.HasPrefix(description.HTML, fmt.Sprintf("<h%d ", level+1)) ||
			!strings.Contains(description.HTML, `id="album"`) {
			t.Fatalf("heading lost escaped text, level or anchor: %s", description.HTML)
		}
	}
}

func TestEveryGalleryExampleHasAnAccurateDescription(t *testing.T) {
	children := map[string][3]string{
		"pk-ui.component.button/with-icon":         {"icon", "IconEnd", "pk-ui.component.icon"},
		"pk-ui.component.button/with-leading-icon": {"icon", "IconStart", "pk-ui.component.icon"},
		"pk-ui.component.badge/warning":            {"icon", "IconStart", "pk-ui.component.icon"},
		"pk-ui.component.alert/warning":            {"icon", "IconStart", "pk-ui.component.icon"},
		"pk-ui.component.emptystate/default":       {"action", "Actions", "pk-ui.component.link"},
		"pk-ui.component.toolbar/default":          {"action", "children", "pk-ui.component.button"},
	}
	for _, example := range examples.Gallery() {
		t.Run(example.ID, func(t *testing.T) {
			description := describeExample(t, example)
			if example.ID == "pk-ui.component.grid/default" {
				if len(description.Children) != 3 || len(description.OpaqueSlots) != 0 || strings.Count(description.HTML, "<p ") != 3 {
					t.Fatalf("grid cells must be distinct, captured Text components: %+v", description)
				}
				for i, id := range []string{"first", "second", "third"} {
					child := description.Children[i]
					if child.Description.ID != id || child.Description.ComponentID != "pk-ui.component.text" || child.Slot != "children" {
						t.Fatalf("grid cell lost its source identity: %+v", child)
					}
					checkCompositionSpan(t, description, child)
				}
			}
			if want, ok := children[example.ID]; ok {
				if len(description.Children) != 1 ||
					[3]string{description.Children[0].Description.ID, description.Children[0].Slot, description.Children[0].Description.ComponentID} != want {
					t.Fatalf("lost the explicitly composed gallery child: %+v", description.Children)
				}
				checkCompositionSpan(t, description, description.Children[0])
			}
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
	sidebar := examples.ExampleOf(exampleInfo, c.SidebarProps{Items: []c.SidebarItem{{Label: "Parent", Children: []c.SidebarItem{{Label: "Child"}}}}}, c.Sidebar)
	if schema := string(describeExample(t, sidebar).Schema); !strings.Contains(schema, `"$defs"`) || !strings.Contains(schema, `"$ref"`) {
		t.Fatalf("recursive SidebarItem data not represented: %s", schema)
	}
}

func TestExampleNodesRemainOpaqueTrustedReferences(t *testing.T) {
	// We copy slot containers, not arbitrary state captured by a Go Node.
	text := "before"
	node := g.NodeFunc(func(w io.Writer) error { _, err := io.WriteString(w, text); return err })
	example := examples.ExampleWithChildren(exampleInfo, c.CardProps{}, []g.Node{node}, c.Card)
	text = "after"
	if !strings.Contains(describeExample(t, example).HTML, "after") {
		t.Fatal("opaque Node capability unexpectedly replaced")
	}
}

func TestExampleTablePatchKeepsJSONNumberPrecision(t *testing.T) {
	example := examples.ExampleOf(exampleInfo, c.TableProps{}, c.Table)
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
		example := examples.ExampleOf(exampleInfo, props{Value: value}, func(props) g.Node { return g.Text("Preview") })
		if _, err := example.Describe(); err == nil {
			t.Errorf("opaque dynamic value %T was silently projected as a data struct", value)
		}
	}
}

func TestExampleRejectsCyclicDataWithoutMutatingIt(t *testing.T) {
	data := map[string]any{}
	data["self"] = data
	example := examples.ExampleOf(exampleInfo, struct{ Data map[string]any }{Data: data}, func(struct{ Data map[string]any }) g.Node { return g.Text("Cycle") })
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
	example := examples.ExampleOf(exampleInfo, props{N: "9007199254740993"}, func(p props) g.Node { return g.Text(p.N.String()) })
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
	example := examples.ExampleOf(exampleInfo, props{}, func(p props) g.Node {
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

func TestSelectLabelKeepsItsOwningPropertyWhenReusingLabel(t *testing.T) {
	example := examples.ExampleOf(exampleInfo, c.SelectProps{Name: "state", Label: "State & <kind>", Required: true,
		Value: "draft", Options: []c.SelectOption{{Value: "draft", Label: "Draft"}}}, c.Select)
	before := describeExample(t, example)
	if !strings.Contains(before.HTML, "<!--pk-text:label-->State &amp; &lt;kind&gt;<!--/pk-text:label-->") ||
		strings.Contains(before.HTML, "<!--pk-text:text-->") {
		t.Fatal("Select borrowed standalone Label's property identity instead of retaining label")
	}
	edited, err := example.WithProps(json.RawMessage(`{"label":"Lifecycle"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`for="pk-select-state"`, `id="pk-select-state"`, `required`,
		`value="draft" selected`, "<!--pk-text:label-->Lifecycle<!--/pk-text:label-->"} {
		if !strings.Contains(describeExample(t, edited).HTML, want) {
			t.Fatalf("label edit lost %s", want)
		}
	}
	if describeExample(t, example).HTML != before.HTML {
		t.Fatal("label edit mutated the original Select")
	}
}

func TestSelectPreservesExactChoiceValues(t *testing.T) {
	options := []c.SelectOption{{Value: "padded", Label: "Plain"}, {Value: " padded ", Label: "Padded"}, {Value: "", Label: "Empty"}}
	for _, multiple := range []bool{false, true} {
		t.Run(fmt.Sprintf("multiple=%t", multiple), func(t *testing.T) {
			props := c.SelectProps{Name: "choice", Value: " padded ", Required: true, Options: options, Multiple: multiple}
			if multiple {
				props.Value, props.Values = "", []string{"", " padded "}
			}
			description := describeExample(t, examples.ExampleOf(exampleInfo, props, c.Select))
			if !strings.Contains(description.HTML, `value=" padded " selected`) || strings.Contains(description.HTML, `value="padded" selected`) {
				t.Fatal("Select changed the selected identifier by trimming it")
			}
			if multiple && !strings.Contains(description.HTML, `value="" selected`) {
				t.Fatal("multiple selection discarded an explicitly selected empty identifier")
			}
		})
	}
}

func TestSelectDeclaresItsSourceChoiceFields(t *testing.T) {
	for _, multiple := range []bool{false, true} {
		example := examples.ExampleOf(exampleInfo, c.SelectProps{Name: "choice", Multiple: multiple,
			Options: []c.SelectOption{{Value: "draft", Label: "Draft"}}}, c.Select)
		before := describeExample(t, example)
		for _, marker := range []string{`data-pk-value="value"`, `data-pk-values="values"`, `data-pk-options="options"`} {
			if strings.Count(before.HTML, marker) != 1 {
				t.Fatalf("Select multiple=%t must declare one owning choice field: %s", multiple, marker)
			}
		}
		if !strings.Contains(before.HTML, `name="choice"`) || !strings.Contains(before.HTML, `value="draft"`) {
			t.Fatal("choice metadata replaced the native control or option")
		}
	}
}
