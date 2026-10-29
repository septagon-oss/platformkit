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

// declaredViews are the screens this resource's own declarations describe, with every
// command it registered that can actually be called, read against mountedOperations
// for the reason view states. The inventory is structure, not one language, so no
// request's locale enters it.
func declaredViews(r httpx.Resource) []screen.View {
	callable := make([]httpx.Command, 0, len(r.Commands))
	for _, c := range r.Commands {
		if c.Run != nil {
			callable = append(callable, c)
		}
	}
	r.Commands = callable
	r.Operations = mountedOperations(r)
	return resource.ScreenViews(resolvedResource(r, Options{}))
}

func describedCommand(r httpx.Resource, c httpx.Command, at string) resource.Command {
	return resource.Command{Bindings: c.Bindings, Verb: c.Verb, Label: c.Summary, Description: c.Description,
		Fields: c.Fields, Collection: c.Collection, Base: at, Present: c.Present,
		ScreenOwner: r.Module, Entity: r.Entity, Repeated: r.History != nil && r.History.RestoreVerb == c.Verb}
}

// commandResponseKind names what a command route answers with: the record it
// changed, or the result the command was asked to compute.
func commandResponseKind(c httpx.Command) string {
	if c.HasResult {
		return "result"
	}
	return "redirect"
}
