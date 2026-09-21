package screens

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// CatalogVersion is the shape of the document at /api/v1/app/resources — the
// contract a shipped native shell renders from — and it is the one number in
// this repository that cannot be revised by the person who changes it.
//
// It goes up when an existing key changes meaning, when a key becomes required,
// and whenever a shell that does not know the change could still parse the
// document and draw the wrong screen. It does not go up for an added optional
// key: an older shell ignores what it has not read, which is the only reason any
// of this can move forward without a flag day.
//
// What the number cannot do is protect a build that is already installed — the
// shell in somebody's pocket parses what it was written to parse. So the rule
// that actually protects it is the one beside this constant: additive, optional,
// and never a change of meaning. A consumer that must refuse is the *next* build,
// reading this field, and that is who the field is written for.
const CatalogVersion = 1

// Catalog is the machine-readable form of what a shell shows: every resource
// the caller may read, its schema, and whether the caller may write it. A shell
// that is not a browser — the native one — generates its screens from this the
// way a browser shell generates them from httpx.Resources, by the same rules.
type Catalog struct {
	// Version is CatalogVersion at the moment the document was written.
	Version   int     `json:"catalogVersion"`
	Resources []Entry `json:"resources"`
}

// Entry is one resource as a shell sees it. There is no Readable field because
// an unreadable resource is not in the document at all: what a caller may not
// look at, they are not told exists.
type Entry struct {
	entity.Schema
	// Screen is the workspace address of the generated screen — /app/task/tasks
	// — stated because the kernel composed it and a shell cannot derive it any
	// more. It used to derive the screen's path from the API's, by cutting
	// "/api/v1" off one and pasting in where it believed the shell to be; the
	// moment a resource's reads and writes stood on different surfaces, that
	// derivation was a guess. It is still optional, so a build already installed
	// keeps working from the address it can derive.
	Screen    string   `json:"screen,omitempty"`
	Immutable []string `json:"immutable,omitempty"`
	Writable  bool     `json:"writable"`
	// WritePath is the address the writes of this resource are answered at, for
	// the one resource whose writes do not answer at its own Path: a price list is
	// read by the tenant that pays for it on the workspace surface and written by
	// the installation on the control plane. It is printed on the same rule as a
	// command's Path — only when the derivation is no longer true, so an entry a
	// shell could always read the same way still reads that way — and only for a
	// caller who may write, because this document is what this caller may do, and
	// an address they cannot reach is neither true nor theirs to be told.
	WritePath string `json:"write_path,omitempty"`
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
// Path is carried only when the derivation is no longer true. A command the
// installation owns lives on the control plane, whose address does not start
// from the resource's own, so POST {Path}/{id}/{verb} would send a caller to an
// address nothing answers at. Absent means what it always meant: derive it that
// way, which is what every command that shares its resource's surface is still
// reachable by — so a document written before this key existed is read the same
// way it always was.
type Command struct {
	Verb        string         `json:"verb"`
	Summary     string         `json:"summary,omitempty"`
	Description string         `json:"description,omitempty"`
	Collection  bool           `json:"collection,omitempty"`
	Path        string         `json:"path,omitempty"`
	Fields      []entity.Field `json:"fields,omitempty"`
}

// Describe is the catalog for this caller: the readable resources, in the
// order given, each saying whether this caller may write it. The two questions
// are the ones the closures on the resource ask — the same Authorizer, the same
// operator rule — so the document cannot promise a screen the API would refuse.
func Describe(ctx context.Context, resources []httpx.Resource) Catalog {
	out := Catalog{Version: CatalogVersion, Resources: []Entry{}}
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
	e := Entry{Schema: r.Schema, Screen: r.Screen, Immutable: r.Immutable, Writable: writable, Singleton: r.Singleton}
	if writable {
		e.WritePath = r.WritePath
	}
	for _, c := range r.Commands {
		e.Commands = append(e.Commands, Command{
			Verb: c.Verb, Summary: c.Summary, Description: c.Description,
			Collection: c.Collection, Path: derived(r, c), Fields: c.Fields,
		})
	}
	return e
}

// derived is the command's address when a shell could not work it out itself,
// and empty when it could. The empty answer is the whole point: this document is
// a contract a build in somebody's pocket parses, and the entries that still
// follow the rule must read exactly as they did before the rule stopped always
// being true.
func derived(r httpx.Resource, c httpx.Command) string {
	if c.Endpoint == "" || c.Endpoint == r.Schema.Path+itemish(r, c)+"/"+c.Verb {
		return ""
	}
	return c.Endpoint
}

func itemish(r httpx.Resource, c httpx.Command) string {
	if c.Collection || r.Singleton {
		return ""
	}
	return "/{id}"
}
