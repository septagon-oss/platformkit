package screens

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// Catalog is the machine-readable form of what a shell shows: every resource
// the caller may read, its schema, and whether the caller may write it. A shell
// that is not a browser — the native one — generates its screens from this the
// way a browser shell generates them from httpx.Resources, by the same rules.
type Catalog struct {
	Resources []Entry `json:"resources"`
}

// Entry is one resource as a shell sees it. There is no Readable field because
// an unreadable resource is not in the document at all: what a caller may not
// look at, they are not told exists.
type Entry struct {
	entity.Schema
	Immutable []string `json:"immutable,omitempty"`
	Writable  bool     `json:"writable"`
	// Commands are the doors this caller may open beyond the five: the
	// lifecycle routes the resource carries. A command the caller may not call
	// is absent for the same reason an unreadable resource is.
	Commands []Command `json:"commands,omitempty"`
	// Singleton says a tenant has one of these, at Path itself: a screen for
	// it is the record and its form, and never a list with a New button on it.
	Singleton bool `json:"singleton,omitempty"`
}

// Command is one lifecycle route as a shell sees it. It carries no permission
// for the same reason Entry carries no readable flag: the document is what this
// caller may do, not a description of the API's guards.
//
// The path is not carried either, because it is derived the way every other
// path here is: POST {Path}/{id}/{verb}, or {Path}/{verb} when Collection. A
// shell that had to be told would be a shell that could be told wrong.
type Command struct {
	Verb        string         `json:"verb"`
	Summary     string         `json:"summary,omitempty"`
	Description string         `json:"description,omitempty"`
	Collection  bool           `json:"collection,omitempty"`
	Fields      []entity.Field `json:"fields,omitempty"`
}

// Describe is the catalog for this caller: the readable resources, in the
// order given, each saying whether this caller may write it. The two questions
// are the ones the closures on the resource ask — the same Authorizer, the same
// operator rule — so the document cannot promise a screen the API would refuse.
func Describe(ctx context.Context, resources []httpx.Resource) Catalog {
	out := Catalog{Resources: []Entry{}}
	for _, r := range resources {
		if !r.Readable(ctx) {
			continue
		}
		// r is this loop's copy, so narrowing its commands to the ones this
		// caller may call leaves Describe1 the pure function it is.
		r.Commands = r.CommandsFor(ctx)
		out.Resources = append(out.Resources, Describe1(r, r.Writable(ctx)))
	}
	return out
}

// Describe1 is one entry, from a resource and the answer to "may this caller
// write it". It is the pure half of Describe, and what the golden test builds
// from without an authorizer.
func Describe1(r httpx.Resource, writable bool) Entry {
	e := Entry{Schema: r.Schema, Immutable: r.Immutable, Writable: writable, Singleton: r.Singleton}
	for _, c := range r.Commands {
		e.Commands = append(e.Commands, Command{
			Verb: c.Verb, Summary: c.Summary, Description: c.Description,
			Collection: c.Collection, Fields: c.Fields,
		})
	}
	return e
}
