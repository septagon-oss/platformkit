package view

import (
	"slices"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// Input is everything Build decides from. It is read and never written: a caller may
// build the same page concurrently for two viewers, and Data.Records come back
// unchanged.
type Input struct {
	// Resource is what the page is about, with no address, markup or transport in
	// it. Whoever holds the registration adapts it; see Description.
	Resource Description
	// Policy is the presentation policy resolved for one language, as the catalogue
	// document carries it.
	Policy present.Resolved
	// Data is the authorised values, already narrowed to what this caller may read.
	Data Data
	// State is the fixed presentation state: kind, language, zone, clock, grant.
	State State
}

// Description is the adapted resource description: what a resource *is*, said by
// whoever holds the registration. It carries no screen, no write path, no command
// path and no API path — a Page gives addresses to the painter — and it carries the
// one thing the resolved policy does not: which arguments a command takes. The
// adapter that fills it from the HTTP resource description lives beside the adapter
// that fills the rendered resource today, not here: a pure contract cannot name a
// transport, and the seam is the other module's to own.
type Description struct {
	// Resource is `module/entity`, the text a field's declared reference already
	// carries. "" is allowed for a page nobody registered.
	Resource string
	// Schema is the registration's own, the source of every type, default, ceiling
	// and map key a control shows.
	Schema entity.Schema
	// Operations are the routes the resource answers, in list/read/create/update/
	// delete order. nil is all five, the rule the catalogue states.
	Operations []string
	// Singleton is a resource with one row per tenant: never listed, never created.
	Singleton bool
	// Immutable names the fields a command owns: shown read-only, never typed.
	Immutable []string
	// Commands are the lifecycle routes the resource offers, in its own order.
	Commands []CommandDescription
}

// CommandDescription is one lifecycle route's shape. Its words, visibility and tone
// are the policy's; what the policy does not carry, and the wire always has, is here.
// Fields are the arguments and nil is none: an action whose body nobody declared is a
// lie with a button on it, so a resolved command with no entry here — or an entry
// with no resolved words — draws no action.
type CommandDescription struct {
	Verb       string // what to send
	Collection bool   // the scope: the collection, or one record
	Fields     []entity.Field
	// Undo names the verb that undoes this one, "" when nothing does.
	Undo string
	// HasResult is true when running it answers a page rather than nothing.
	HasResult bool
}

// Data is the authorised values. Records come in the order the read returned them
// and Build never re-sorts them; a record or a form page reads Records[0] and
// ignores the rest. Sort is the exact `sort` value the read used, "" when none was
// sent. Violations and Detail are a refused write's, and their presence is what
// makes a form page a 422 rather than a blank form.
type Data struct {
	Records []map[string]any
	// Window is the slice the read was asked for.
	Window  Window
	Total   int64
	Counted bool
	Sort    string
	// Violations is one sentence per field, keyed by the field's own name.
	Violations map[string]string
	Detail     string
}

// State is the fixed presentation state. Language must be the tag Policy was
// resolved for, or the page would mix two languages, and it is never defaulted here.
// Zone is the location the *caller* loaded and nil is UTC: Build loads no zone,
// because loading one reads /usr/share/zoneinfo and a builder performs no I/O. Now
// is the instant the page is stamped builtAt, from the composition's clock — the
// clock a viewer's device holds is not a fact the platform may borrow, and a relative
// phrase is painting anchored on it. Writable is the caller's own write grant, which
// Build reads only to decide which doors to draw: a page is not an authority and
// checks nothing. Home is the breadcrumb root's label, "" for no root crumb.
type State struct {
	Kind     Kind
	Language string
	Zone     *time.Location
	Now      time.Time
	Writable bool
	Home     string
	// Form is the part of the state that means something to a form page only.
	Form FormState
}

// FormState is a form page's own state: Create selects the create layout and false
// the update one. Title is the caller's words, from the caller's own copy seam: this
// package names no key of its own for "New %s" or "Edit %s".
type FormState struct {
	Create bool
	Title  string
}

// offers reports whether the resource answers one operation. A singleton is readable
// and updatable only, whatever its routes answer, because a tenant's one row is never
// created and never a list.
func (d Description) offers(op string) bool {
	if d.Singleton && op != "update" && op != "read" {
		return false
	}
	return d.Operations == nil || slices.Contains(d.Operations, op)
}
