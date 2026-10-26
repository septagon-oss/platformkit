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

// resolvedView is the control decision the resolved policy already made for one
// screen. A caller who may not write is shown no door that would refuse them: the
// routes are the resource's, and this is where the person's half comes off.
func (r Resource) resolvedView(kind string, writable bool) present.ResolvedView {
	for _, v := range r.Resolved.Views {
		if v.Kind != kind {
			continue
		}
		if !writable {
			v.Controls = slices.DeleteFunc(slices.Clone(v.Controls), func(c present.ResolvedControl) bool {
				return c.Name == "action.create" || c.Name == "action.edit" || c.Name == "action.delete"
			})
		}
		return v
	}
	return present.ResolvedView{Kind: kind, Cardinality: screen.One}
}

func hasControl(v present.ResolvedView, name string) bool {
	return slices.ContainsFunc(v.Controls, func(c present.ResolvedControl) bool { return c.Name == name })
}

// input is the command as the resolver sees it: its words, and the arguments and
// shape that decide which controls its view names.
func (c Command) input(hints entity.CommandHints) present.CommandInput {
	return present.CommandInput{Verb: c.Verb, Summary: c.Label, Hints: hints,
		Fields: c.Fields, Bindings: c.Bindings, Collection: c.Collection, Repeated: c.Repeated}
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

// ScreenViews derives immutable descriptors from the same control decisions the
// renderer consumes: the resolved policy named them, and ScreenOwner names the
// module that declared them. A resource that carries no supported policy resolves
// its own declarations first, so the publication is the composition's contract and
// never one language's or one caller's.
func ScreenViews(r Resource) []screen.View {
	if r.ScreenOwner == "" {
		return nil
	}
	r = r.policy(Options{})
	out := make([]screen.View, 0, len(r.Resolved.Views))
	for _, v := range r.Resolved.Views {
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
