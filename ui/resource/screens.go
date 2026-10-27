package resource

import (
	"cmp"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

// resolvedView is the control decision the resolved policy made for one screen, or
// the record's own form where its route holds no screen.
func (r Resource) resolvedView(kind string) present.ResolvedView {
	for _, v := range r.Resolved.Views {
		if v.Kind == kind {
			return v
		}
	}
	if verb, isForm := formVerb[kind]; isForm {
		if v := r.recordForm(kind, verb); v.Kind != "" {
			return v
		}
	}
	return present.ResolvedView{Kind: kind, Cardinality: screen.One}
}

// input is the command as the resolver sees it: its words, and the arguments and
// shape that decide which controls its view names.
func (c Command) input(hints entity.CommandHints) present.CommandInput {
	return present.CommandInput{Verb: c.Verb, Summary: c.Label, Hints: hints,
		Fields: c.Fields, Bindings: c.Bindings, Collection: c.Collection, Repeated: c.Repeated}
}

// genericRef is the screen a module's own entity names, and nothing for a render
// that declares no screen owner. A ref that cannot be spelled is a bug in the module
// that declared it, never a request's mistake.
func genericRef(owner, entityName, kind, verb string) screen.ViewRef {
	if owner == "" {
		return ""
	}
	ref, err := screen.Generic("app", owner, entityName, kind, verb)
	if err != nil {
		panic(err)
	}
	return ref
}

func (r Resource) ref(kind, verb string) screen.ViewRef {
	return genericRef(r.ScreenOwner, r.Schema.Entity, kind, verb)
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

// formVerb names the write a form view posts, which is not the kind's own name: an
// edit form posts update. Any other view posts the verb it is named for.
var formVerb = map[string]string{"create": "create", "edit": "update"}

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
	operation := op + cmp.Or(formVerb[v.Kind], v.Verb)
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
