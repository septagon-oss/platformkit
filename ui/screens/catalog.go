package screens

import (
	"context"
	"encoding/json"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// CatalogVersion is the shape of the document at /api/v1/admin/resources — the
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
	// JSONSchema is those same fields in JSON Schema 2020-12, for the consumer
	// that reads a schema it did not compile against — an MCP tool, a generated
	// client, a validator — rather than learning FieldType to use one. Fields
	// stays: it says which control draws the value and which screen shows it,
	// which a schema for the record has no business saying. It is not called
	// Schema because the embedded entity.Schema already owns that name.
	JSONSchema json.RawMessage `json:"schema,omitempty"`
	Immutable  []string        `json:"immutable,omitempty"`
	Writable   bool            `json:"writable"`
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
	// Schema is Fields as JSON Schema 2020-12: the body this command takes, for
	// a client that will not compile against the entity either. A command with
	// no argument has no fields and so no schema, which is the same answer as
	// "POST it with no body".
	Schema json.RawMessage `json:"schema,omitempty"`
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
	e := Entry{
		Schema: r.Schema, JSONSchema: mustSchema(r.Entity, r.Schema.Fields),
		Immutable: r.Immutable, Writable: writable, Singleton: r.Singleton,
	}
	for _, c := range r.Commands {
		e.Commands = append(e.Commands, Command{
			Verb: c.Verb, Summary: c.Summary, Description: c.Description,
			Collection: c.Collection, Fields: c.Fields,
			Schema: mustSchema(r.Entity+" command "+c.Verb, c.Fields),
		})
	}
	return e
}

// mustSchema is the projection, or the reason this request fails.
//
// The one way entity.JSONSchema refuses is a `default:"…"` that does not parse
// as the type beside it, which is a mistake in the declaration rather than in
// anything a caller did. It is the same class as a widget name no renderer
// knows, and kit/rest already refuses to mount over that one. Refusing loudly is
// the alternative to serving a catalog that left a schema out of the entry a
// shell is generating its screens from. kit/httpx's own recovery turns the panic
// into a 500 and a log line naming the field, so a bad declaration costs one
// request and says which field to go and fix.
func mustSchema(what string, fields []entity.Field) json.RawMessage {
	if len(fields) == 0 {
		return nil
	}
	doc, err := entity.JSONSchema(fields)
	if err != nil {
		panic("screens: " + what + ": " + err.Error())
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		panic("screens: " + what + ": the projected schema did not encode: " + err.Error())
	}
	return raw
}
