package screens

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func screenRef(r httpx.Resource, kind, verb string) screen.ViewRef {
	ref, err := screen.Generic("app", r.Module, r.Entity, kind, verb)
	if err != nil {
		panic(err)
	}
	return ref
}

func declaredViews(r httpx.Resource) []screen.View {
	// The inventory is structure, not one language: no request's locale enters it, so
	// it resolves with no Options at all.
	v := described(r, Options{})
	for _, c := range r.Commands {
		if c.Run == nil || c.Present.System {
			continue
		}
		v.Commands = append(v.Commands, describedCommand(r, c, r.Screen))
	}
	return resource.ScreenViews(v)
}

func describedCommand(r httpx.Resource, c httpx.Command, at string) resource.Command {
	label := c.Present.Label
	if label == "" {
		label = c.Summary
	}
	return resource.Command{Bindings: c.Bindings, Verb: c.Verb, Label: label, Description: c.Description,
		Fields: c.Fields, Collection: c.Collection, Base: at, Present: c.Present,
		ScreenOwner: r.Module, Entity: r.Entity, Repeated: r.History != nil && r.History.RestoreVerb == c.Verb}
}

func commandResponse(c httpx.Command) string {
	if c.HasResult {
		return "result"
	}
	return "redirect"
}
