package screens

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/crud"
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
	crud.Schema
	Immutable []string `json:"immutable,omitempty"`
	Writable  bool     `json:"writable"`
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
		out.Resources = append(out.Resources, Describe1(r, r.Writable(ctx)))
	}
	return out
}

// Describe1 is one entry, from a resource and the answer to "may this caller
// write it". It is the pure half of Describe, and what the golden test builds
// from without an authorizer.
func Describe1(r httpx.Resource, writable bool) Entry {
	return Entry{Schema: r.Schema, Immutable: r.Immutable, Writable: writable}
}
