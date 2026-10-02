package export

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
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
)

// Options is what one request to a gallery decides about the words framing its
// specimens. The gallery chrome — the landmark around the list of examples, the
// theme and width menus, the summary over the Go snippet — is copy this layer
// wrote, and the tenant reading it may be served in a language this layer has a
// file for and not another. The specimen inside the frame is data its author
// wrote in one language, and this package asks for no word of it.
type Options struct {
	// Locale is this request's selected language, as the shell negotiated it.
	// Nil keeps the source English, which is what a composition with no catalog
	// is served today.
	Locale *locale.Locale
}

// Label is one word of the gallery's own chrome under its gallery. prefix, in the
// language this request is answered in. The fallback is the source English the
// call site is written in, so a composition that ships no ui/export catalogue
// still reads exactly what it always read — and, because that English is the
// pseudo-locale's source, a deployment that does ship one sees these sentences
// accented like every other translated one.
func (o Options) Label(key, fallback string, args ...any) string {
	if o.Locale == nil {
		if len(args) > 0 {
			return fmt.Sprintf(fallback, args...)
		}
		return fallback
	}
	return o.Locale.Text("gallery."+key, fallback, args...)
}

// Storybook is one application's selected design composition. The application
// authorizes it before passing it to a gallery or Export; this value neither
// knows nor discovers other tenants. Keep tenant assets in authorized application
// routes or embed them in the examples, never in the shell's public static tree.
type Storybook struct {
	Title    string
	Theme    design.Pair
	Examples []examples.Example
	Extra    []ui.Extra
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
func (b Storybook) Find(id string) (examples.Example, bool) {
	for _, example := range b.Examples {
		if example.ID == id {
			return example, true
		}
	}
	return examples.Example{}, false
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
func StorybookPage(b Storybook, o Options, path, group string, selected examples.Example, props, mode, width string) (g.Node, error) {
	var links []g.Node
	for _, name := range append([]string{""}, b.groups()...) {
		label := name
		if name == "" {
			label = o.Label("all", "All")
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
	index := h.Nav(g.Attr("aria-label", o.Label("landmark", "Component examples")), h.Ul(g.Group(choices)))
	var detail g.Node = components.Text(components.TextProps{
		Content: o.Label("empty", "No examples are published for this selection.")})
	if selected.ID != "" {
		var err error
		detail, err = storybookExample(selected, o, path, group, props, mode, width)
		if err != nil {
			return nil, err
		}
	}
	return h.Div(g.Attr("data-gallery", ""),
		components.Stack(components.StackProps{Gap: "6"},
			components.SectionHeader(components.SectionHeaderProps{Title: b.Title, Level: 1,
				Description: o.Label("description", "Explore components, adjust their properties, and test light, dark, and responsive layouts.")}),
			g.If(b.Files != nil, components.Link(components.LinkProps{Label: o.Label("open_storybook", "Open Storybook"), Href: path + "/storybook/index.html"})),
			components.Flex(components.FlexProps{Wrap: true, Gap: "3"}, links...),
			components.Link(components.LinkProps{Label: o.Label("download", "Download design export"), Href: path + "/export"}),
			h.Div(g.Attr("data-gallery-layout", ""), index, detail))), nil
}

func storybookExample(example examples.Example, o Options, path, group, props, mode, width string) (g.Node, error) {
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
		components.Select(components.SelectProps{Name: "theme", Label: o.Label("preview_theme", "Preview theme"), Value: mode, Required: true,
			Options: []components.SelectOption{{Value: "light", Label: o.Label("light", "Light")},
				{Value: "dark", Label: o.Label("dark", "Dark")},
				{Value: "system", Label: o.Label("system", "System")}}}),
		components.Select(components.SelectProps{Name: "width", Label: o.Label("preview_width", "Preview width"), Value: width, Required: true,
			Options: []components.SelectOption{{Value: "fit", Label: o.Label("fit", "Fit available space")},
				{Value: "320", Label: o.Label("phone", "Phone · 320px")},
				{Value: "768", Label: o.Label("tablet", "Tablet · 768px")},
				{Value: "1280", Label: o.Label("desktop", "Desktop · 1280px")}}}),
	}
	if d.PropsEditable {
		form = append(form, propertyControls(d, o)...)
	}
	form = append(form, components.Button(components.ButtonProps{Label: o.Label("apply", "Apply preview"), Type: "submit"}),
		components.Link(components.LinkProps{Label: o.Label("reset", "Reset properties"), Href: path + "?" + url.Values{"example": {example.ID}, "group": {group}}.Encode()}))
	var snippet g.Node
	if code, err := example.GoProps(); err == nil {
		snippet = h.Details(h.Summary(g.Text(o.Label("go_properties", "Go properties"))),
			components.Textarea(components.TextareaProps{Name: "go-props", Label: o.Label("copyable_properties", "Copyable Go properties"), Value: code, ReadOnly: true, Rows: 8, FullWidth: true}),
			components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-gallery-copy": ""}}, Label: o.Label("copy_properties", "Copy Go properties"), Variant: "secondary"}))
	}
	return h.Article(components.Stack(components.StackProps{Gap: "4"},
		components.Heading(components.HeadingProps{Text: example.Name, Level: 2}),
		components.Text(components.TextProps{Content: o.Label("local_only", "Preview interactions stay local. Network requests and form submissions are disabled."), Size: "sm", Color: "muted"}),
		h.Div(g.Attr("data-gallery-viewport", ""), h.IFrame(h.Title(o.Label("preview_title", "%s preview", example.Name)), h.Src(preview), g.Attr("sandbox", "allow-scripts"), g.Attr("data-gallery-width", width))),
		components.Link(components.LinkProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-gallery-preview-link": ""}}, Label: o.Label("open_preview", "Open preview"), Href: preview, External: true}),
		h.Details(h.Open(), h.Summary(g.Text(o.Label("preview_controls", "Preview controls"))), h.Form(form...)),
		h.Div(h.ID("pk-gallery-status"), h.Role("status"), g.Attr("data-gallery-status", "")), snippet,
		h.Details(h.Summary(g.Text(o.Label("properties_slots", "Properties and slots"))), examples.Documentation(example)))), nil
}

func propertyControls(d examples.ExampleDescription, o Options) []g.Node {
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
					label = o.Label("default", "Default")
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
