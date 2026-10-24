package resource

import (
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func (r Resource) presentation(writable bool) present.Resolved {
	ops := r.Operations
	if !writable {
		if ops == nil {
			ops = []string{"list", "read"}
		}
		ops = slices.DeleteFunc(slices.Clone(ops), func(op string) bool { return op != "list" && op != "read" })
	}
	commands := make([]present.CommandInput, 0, len(r.Commands))
	for _, c := range r.Commands {
		commands = append(commands, c.input())
	}
	return present.Resolve(present.Input{Schema: r.Schema, Hints: r.Present, Immutable: r.Immutable,
		Singleton: r.Singleton, Operations: ops, Commands: commands})
}

func (c Command) input() present.CommandInput {
	return present.CommandInput{Verb: c.Verb, Summary: c.Label, Hints: c.Present,
		Fields: c.Fields, Bindings: c.Bindings, Collection: c.Collection, Repeated: c.Repeated}
}

func (r Resource) resolvedView(kind string, writable bool) present.ResolvedView {
	for _, v := range r.presentation(writable).Views {
		if v.Kind == kind {
			return v
		}
	}
	if kind == "create" || kind == "edit" {
		input := present.Input{Schema: r.Schema, Hints: r.Present, Immutable: r.Immutable, Operations: []string{"create", "update"}}
		for _, v := range present.Resolve(input).Views {
			if v.Kind == kind {
				return v
			}
		}
	}
	return present.ResolvedView{Kind: kind, Cardinality: screen.One}
}

func hasControl(v present.ResolvedView, name string) bool {
	return slices.ContainsFunc(v.Controls, func(c present.ResolvedControl) bool { return c.Name == name })
}

func (r Resource) ref(kind, verb string) screen.ViewRef {
	if r.ScreenOwner == "" {
		return ""
	}
	ref, err := screen.Generic("app", r.ScreenOwner, r.Schema.Entity, kind, verb)
	if err != nil {
		panic(err)
	}
	return ref
}

func (r Resource) anchor(kind, name string) map[string]string {
	ref := r.ref(kind, "")
	if ref == "" {
		return nil
	}
	return map[string]string{"data-screen-anchor": string(ref) + "#" + name}
}

func (r Resource) viewBody(kind string, body []g.Node) []g.Node {
	if ref := r.ref(kind, ""); ref != "" {
		return []g.Node{components.Stack(components.StackProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-screen-view": string(ref)}}, Gap: "6"}, body...)}
	}
	return body
}

// ScreenViews derives immutable descriptors from the same control decisions
// the renderer consumes. ScreenOwner must name the declaring module.
func ScreenViews(r Resource) []screen.View {
	if r.ScreenOwner == "" {
		return nil
	}
	out := []screen.View{}
	for _, v := range r.presentation(true).Views {
		out = append(out, r.descriptor(v))
	}
	return out
}

func (r Resource) descriptor(v present.ResolvedView) screen.View {
	ref := r.ref(v.Kind, v.Verb)
	spec := screen.ViewSpec{Ref: ref, Cardinality: v.Cardinality}
	if v.Kind == "command" {
		host := "detail"
		for _, c := range r.Commands {
			if c.Verb == v.Verb && c.Collection {
				host = "list"
			}
		}
		spec.Hosts = []screen.ViewRef{r.ref(host, "")}
	}
	op := "screen-" + r.ScreenOwner + "-" + r.Schema.Entity + "-"
	operation := op + v.Verb
	if v.Kind == "create" {
		operation = op + "create"
	}
	if v.Kind == "edit" {
		operation = op + "update"
	}
	for _, c := range v.Controls {
		name := c.Name
		var binding screen.Binding
		switch {
		case name == "field":
			anchor, err := ref.Anchor("field", c.Path...)
			if err != nil {
				panic(err)
			}
			_, name, _ = strings.Cut(string(anchor), "#")
			binding = screen.Value(operation, c.Path)
			if !c.Editable {
				binding = screen.Display("resource/" + encodedSegment(r.ScreenOwner) + "/" + encodedSegment(r.Schema.Entity) + "/field/" + strings.TrimPrefix(name, "field."))
			}
		case name == "command":
			anchor, err := ref.Anchor("command", c.Path...)
			if err != nil {
				panic(err)
			}
			_, name, _ = strings.Cut(string(anchor), "#")
			binding = screen.Submission(operation)
		case name == "action.submit":
			binding = screen.Submission(operation)
		case name == "action.delete":
			binding = screen.Submission(op + "delete")
		default:
			kind := strings.TrimPrefix(name, "action.")
			if kind == "cancel" {
				kind = "list"
				if r.Singleton {
					kind = "detail"
				}
			}
			binding = screen.Navigation(r.ref(kind, ""))
		}
		spec.Anchors = append(spec.Anchors, screen.AnchorSpec{Name: name, Control: c.Control, Binding: binding, Interactions: c.Interactions})
	}
	return screen.MustView(spec)
}

func controlField(schema entity.Schema, c present.ResolvedControl) entity.Field {
	for _, f := range schema.Fields {
		if f.Name != c.Path[0] {
			continue
		}
		if len(c.Path) == 2 {
			f.Name = f.Name + "[" + c.Path[1] + "]"
			f.Type, f.Keys = f.Elem, nil
		}
		f.Widget = c.Control
		return f
	}
	panic("resource: resolved field is absent from schema")
}

func encodedSegment(value string) string {
	encoded, err := screen.Segment(value)
	if err != nil {
		panic(err)
	}
	return encoded
}
