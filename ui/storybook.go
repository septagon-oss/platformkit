package ui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
)

// Storybook is one application's selected design composition. The application
// authorizes it before passing it to a gallery or Export; this value neither
// knows nor discovers other tenants. Keep tenant assets in authorized application
// routes or embed them in the examples, never in the shell's public static tree.
type Storybook struct {
	Title    string
	Theme    design.Pair
	Examples []components.Example
	Extra    []Extra
	// Files is an optional, immutable Storybook.js build produced from Export
	// of this exact composition. All files are served through gallery authorization.
	Files fs.FS
}

// Validate refuses ambiguous identities. Empty collections stay empty.
func (b Storybook) Validate() error {
	seen := map[string]bool{}
	for _, example := range b.Examples {
		if example.ID == "" || example.ComponentID == "" || seen[example.ID] {
			return fmt.Errorf("storybook: missing or duplicate identity %q", example.ID)
		}
		seen[example.ID] = true
	}
	return nil
}

// Find resolves strictly inside the selected composition, with no Core fallback.
func (b Storybook) Find(id string) (components.Example, bool) {
	for _, example := range b.Examples {
		if example.ID == id {
			return example, true
		}
	}
	return components.Example{}, false
}

func (b Storybook) groups() []string {
	var groups []string
	for _, example := range b.Examples {
		if !slices.Contains(groups, example.Group) {
			groups = append(groups, example.Group)
		}
	}
	return groups
}

// StorybookPage renders the index and one live example. Previews use their own
// document so a modal can own focus without taking over the documentation.
// The server validates the selected ID and applies Props before calling this.
func StorybookPage(b Storybook, path, group string, selected components.Example, props, mode, width string) (g.Node, error) {
	var links []g.Node
	for _, name := range append([]string{""}, b.groups()...) {
		label := name
		if name == "" {
			label = "All"
		}
		links = append(links, components.Link(components.LinkProps{Label: label, Href: path + "?" + url.Values{"group": {name}}.Encode()}))
	}
	var choices []g.Node
	for _, example := range b.Examples {
		if group != "" && example.Group != group {
			continue
		}
		attrs := []g.Node{h.Href(path + "?" + url.Values{"group": {group}, "example": {example.ID}}.Encode()), g.Text(example.Name)}
		if example.ID == selected.ID {
			attrs = append(attrs, g.Attr("aria-current", "page"))
		}
		choices = append(choices, h.Li(h.A(attrs...)))
	}
	index := h.Nav(g.Attr("aria-label", "Component examples"), h.Ul(g.Group(choices)))
	var detail g.Node = components.Text(components.TextProps{Content: "No examples are published for this selection."})
	if selected.ID != "" {
		var err error
		detail, err = storybookExample(selected, path, group, props, mode, width)
		if err != nil {
			return nil, err
		}
	}
	return h.Div(g.Attr("data-gallery", ""),
		components.Stack(components.StackProps{Gap: "6"},
			components.SectionHeader(components.SectionHeaderProps{Title: b.Title, Level: 1, Description: "Explore components, adjust their properties, and test light, dark, and responsive layouts."}),
			g.If(b.Files != nil, components.Link(components.LinkProps{Label: "Open Storybook", Href: path + "/storybook/index.html"})),
			components.Flex(components.FlexProps{Wrap: true, Gap: "3"}, links...),
			components.Link(components.LinkProps{Label: "Download design export", Href: path + "/export"}),
			h.Div(g.Attr("data-gallery-layout", ""), index, detail))), nil
}

func storybookExample(example components.Example, path, group, props, mode, width string) (g.Node, error) {
	d, err := example.Describe()
	if err != nil {
		return nil, err
	}
	query := url.Values{"example": {example.ID}, "theme": {mode}, "props": {props}}
	preview := path + "/preview?" + query.Encode()
	form := []g.Node{
		h.Method("get"), h.Action(path), g.Attr("data-gallery-controls", ""),
		h.Input(h.Type("hidden"), h.Name("example"), h.Value(example.ID)),
		h.Input(h.Type("hidden"), h.Name("group"), h.Value(group)),
		h.Input(h.Type("hidden"), h.Name("props"), h.Value(props)),
		components.Select(components.SelectProps{Name: "theme", Label: "Preview theme", Value: mode, Required: true, Options: []components.SelectOption{{Value: "light", Label: "Light"}, {Value: "dark", Label: "Dark"}, {Value: "system", Label: "System"}}}),
		components.Select(components.SelectProps{Name: "width", Label: "Preview width", Value: width, Required: true, Options: []components.SelectOption{{Value: "fit", Label: "Fit available space"}, {Value: "320", Label: "Phone · 320px"}, {Value: "768", Label: "Tablet · 768px"}, {Value: "1280", Label: "Desktop · 1280px"}}}),
	}
	if d.PropsEditable {
		form = append(form, propertyControls(d)...)
	}
	form = append(form, components.Button(components.ButtonProps{Label: "Apply preview", Type: "submit"}),
		components.Link(components.LinkProps{Label: "Reset properties", Href: path + "?" + url.Values{"example": {example.ID}, "group": {group}}.Encode()}))
	var snippet g.Node
	if code, err := example.GoProps(); err == nil {
		snippet = h.Details(h.Summary(g.Text("Go properties")),
			components.Textarea(components.TextareaProps{Name: "go-props", Label: "Copyable Go properties", Value: code, ReadOnly: true, Rows: 8, FullWidth: true}),
			components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-gallery-copy": ""}}, Label: "Copy Go properties", Variant: "secondary"}))
	}
	return h.Article(components.Stack(components.StackProps{Gap: "4"},
		components.Heading(components.HeadingProps{Text: example.Name, Level: 2}),
		components.Text(components.TextProps{Content: "Preview interactions stay local. Network requests and form submissions are disabled.", Size: "sm", Color: "muted"}),
		h.Div(g.Attr("data-gallery-viewport", ""), h.IFrame(h.Title(example.Name+" preview"), h.Src(preview), g.Attr("sandbox", "allow-scripts"), g.Attr("data-gallery-width", width))),
		components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-gallery-preview-link": ""}}, Label: "Open preview", Href: preview, External: true}),
		h.Details(h.Open(), h.Summary(g.Text("Preview controls")), h.Form(form...)),
		h.Div(h.ID("pk-gallery-status"), h.Role("status"), g.Attr("data-gallery-status", "")), snippet,
		h.Details(h.Summary(g.Text("Properties and slots")), components.Documentation(example)))), nil
}

func propertyControls(d components.ExampleDescription) []g.Node {
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(d.Schema, &schema) != nil || json.Unmarshal(d.Props, &values) != nil {
		return nil
	}
	var controls []g.Node
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		property := schema.Properties[name]
		kind, _ := property["type"].(string)
		value := string(values[name])
		if value == "" {
			value = "null"
		}
		if kind == "string" {
			_ = json.Unmarshal(values[name], &value)
			if len(values[name]) == 0 {
				value = ""
			}
		}
		attrs := map[string]string{"data-gallery-prop": name, "data-gallery-kind": kind}
		label := name
		if description, ok := property["description"].(string); ok {
			label += " — " + description
		}
		choices, enumerated := property["enum"].([]any)
		if kind == "boolean" {
			choices, enumerated = []any{false, true}, true
			if len(values[name]) == 0 {
				value = "false"
			}
		}
		if enumerated {
			var options []components.SelectOption
			for _, choice := range choices {
				text := fmt.Sprint(choice)
				label := text
				if text == "" {
					label = "Default"
				}
				options = append(options, components.SelectOption{Value: text, Label: label})
			}
			if value == "null" {
				value = "0"
			}
			controls = append(controls, components.Select(components.SelectProps{ComponentProps: components.ComponentProps{Attrs: attrs}, Name: "prop-" + name, Label: label, Value: value, Options: options}))
		} else if kind == "string" || kind == "integer" || kind == "number" {
			typ := "text"
			if kind != "string" {
				typ = "number"
				if value == "null" {
					value = "0"
				}
			}
			controls = append(controls, components.Input(components.InputProps{ComponentProps: components.ComponentProps{Attrs: attrs}, Name: "prop-" + name, Label: label, Value: value, Type: typ, FullWidth: true}))
		} else {
			controls = append(controls, components.Textarea(components.TextareaProps{ComponentProps: components.ComponentProps{Attrs: attrs}, Name: "prop-" + name, Label: label + " (JSON)", Value: value, Rows: 2, FullWidth: true}))
		}
	}
	return controls
}

// StorybookCSS is documentation framing. Component appearances still come from
// Compose, Gallery and the selected application's Extra values.
func StorybookCSS() string {
	return strings.Join([]string{
		"[data-gallery-layout]{display:grid;grid-template-columns:minmax(12rem,1fr) minmax(0,3fr);gap:2rem}",
		"[data-gallery-layout] nav ul{display:grid;gap:.75rem}[data-gallery-layout] a[aria-current]{font-weight:700;text-decoration:underline}",
		"[data-gallery-layout] nav{max-height:calc(100vh - 12rem);overflow:auto;position:sticky;top:1.5rem}",
		"[data-gallery] a:focus-visible,[data-gallery] summary:focus-visible{outline:2px solid var(--pk-color-focus);outline-offset:3px}",
		"[data-gallery-controls]{display:grid;gap:1rem;margin-top:1rem}[data-gallery] summary{cursor:pointer}",
		"[data-gallery-viewport]{overflow:auto;background:var(--pk-color-surface-muted);border:1px solid var(--pk-color-border-default)}",
		"[data-gallery-viewport] iframe{display:block;width:100%;height:32rem;background:var(--pk-color-surface-canvas)}",
		"[data-gallery-width='320']{width:320px!important}[data-gallery-width='768']{width:768px!important}[data-gallery-width='1280']{width:1280px!important}",
		"@media(max-width:767px){[data-gallery-layout]{grid-template-columns:minmax(0,1fr)}[data-gallery-layout] nav{position:static;max-height:12rem;overflow:auto}}",
	}, "\n")
}
