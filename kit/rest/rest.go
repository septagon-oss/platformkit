// Package rest is an entity's projection onto HTTP: five routes, the commands
// beside them, and the one mapping from kit/crud's errors to statuses.
//
// A module declares its entity in contracts/ and one Spec in its manifest.
// Spec supplies list, create, read, update and delete routes with declared
// permissions and events.
//
// Keeping routes separate from entity contracts lets consumers name an entity
// without importing HTTP dependencies. See ARCHITECTURE.md.
//
// The [Spec.Mount] example registers a resource and inspects its routes.
// The [Command] example adds a command and inspects its authorization and address.
package rest

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

// FileUses rewrites the file references one richtext field makes, inside the
// transaction that writes the field. kit/rest declares it rather than importing
// a file module: the kit may not know what a file is, and the composition wires
// the one implementation the application has. It is the same shape as
// richtext.Files beside it, for the same reason.
type FileUses interface {
	SetUses(ctx context.Context, tx db.Tx[db.Tenant], in UsesInput) error
}

// UsesInput names one field of one record, in one locale, and the files it shows
// in document order. Repeats collapse to one use at the far end.
type UsesInput struct {
	Module, Entity, Field, Locale string
	Record                        uuid.UUID
	Files                         []uuid.UUID
	// Public is the audience this record asserts for what this write files. It is
	// the record's state and never the request's claim: kit/rest fills it from
	// Spec.Audience, so no body can make its own images public. False is every
	// write a Spec with no Audience makes, which is every write there was before it.
	Public bool
}

// Spec is one entity's presence in the application: five routes, two
// permissions, three events and a schema. A module writes one of these and
// mounts it; everything below is the same for every entity, which is why it is
// written once.
type Spec[T crud.Entity] struct {
	// input is the typed read-only query declared by ReadForm; nil for every
	// ordinary CRUD Spec, which is what every other field on this struct reads.
	input *readInputSpec
	// Choices supplies finite field declarations copied into this Spec schema.
	Choices *entity.ChoiceFields
	// Source replaces storage reads with a service's scope and projection.
	// Nil keeps ordinary CRUD. See ReadSource for required callbacks and limits.
	Source *ReadSource[T]
	// RichTextFiles resolves richtext image references in the request transaction.
	RichTextFiles richtext.Files
	// FileUses records which file a richtext field of this resource references,
	// in the same transaction that writes the field. It is what a file module
	// keeps so that it can answer "is anything showing this" at all. A Spec with
	// a richtext field and no FileUses is not refused: it takes RecordNoUses,
	// which writes every body and keeps no ledger, and mount logs that once —
	// the day a release sweep exists to delete a file nobody reads, a resource on
	// that default is a resource whose images the sweep is entitled to delete
	// under a published page, which is when this becomes a mount-time refusal.
	FileUses FileUses
	// History keeps a version of every write this Spec commits, in the same
	// transaction as the write. It is a declaration and not a request: a resource
	// whose content people revise wants a version per save, and a resource whose
	// rows are counters does not. It requires two things, both refused at mount:
	// Versions to keep them in, and a write count of the row's own to number them
	// with — the version number is entity.RevisionField, and this module refuses
	// to invent a second one.
	History bool
	// Versions is the store a History resource's versions are kept in, called
	// inside the transaction that wrote the row. modules/history provides it.
	Versions httpx.Versions
	// Translations is the door a Spec with a `i18n:"translatable"` field knocks
	// on: ?lang= reads through it, its commands write through it, and its delete
	// forgets the record through it. modules/translation implements it and nothing
	// else does; the composition assigns it by hand, the way RichTextFiles is
	// assigned, and check() refuses either half wired without the other.
	Translations Translations
	// Module is the manifest's name. It prefixes the events, so the events a
	// Spec publishes are namespaced by the module that mounts it.
	Module string
	// Entity is this resource's name, lower-case: "task". It is the middle of
	// the event name and the noun in the operation ids.
	Entity string
	// Path is the collection's path relative to the module: "/tasks". The item
	// is Path + "/{id}".
	//
	// It is relative and not absolute because the address a route answers at is
	// not the module's to know: the kernel composes it from this path, the
	// module's name and the surface the permissions put it on — /api/v1/task/tasks
	// for a workspace resource, /api/v1/ops/billing/plans for one the
	// installation owns. Writing the prefix here was a module naming a surface
	// it had not chosen.
	Path string
	// Read guards the list and the read; Write guards create, update and
	// delete. Both are permissions some module has to define, or the app
	// refuses to start.
	Read, Write string
	// OperatorRead restricts reads and discovery to the operator's own tenant.
	// The manifest must declare Read with Operator: true. Writes retain their
	// own declaration; private control-plane rows normally set both flags.
	OperatorRead bool
	// OperatorWrite restricts writes to the operator's own tenant, independently
	// of reads. The manifest must declare Write with Operator: true; otherwise
	// kit/app refuses startup. See docs/adr/0008.
	OperatorWrite bool
	// Operations selects the CRUD routes; empty enables all five. Unoffered verbs
	// have no route or generated action, and a list that offers no read draws its
	// rows unlinked, because no record page exists to link them to. Without List
	// there is no generated workspace page. This is a resource declaration,
	// independent of each caller's permission to use the offered routes.
	Operations []httpx.CRUD
	// ReadAuth and WriteAuth state the guard as an httpx.Auth, which is the one
	// spelling a registered resource keeps: Read/Write and the operator flags are
	// inputs to the same derivation and are refused beside a declared guard, not
	// overridden by one. Public guards are refused. SignedIn reads rely on tenant
	// RLS; generic row writes require a grant. Use Command for writes needing an
	// object-policy decision.
	ReadAuth httpx.Auth
	// WriteAuth is the write side of the same two declarations.
	WriteAuth httpx.Auth
	// SoftDelete keeps deleted rows, hidden, instead of removing them.
	SoftDelete bool
	// Immutable lists JSON fields owned by dedicated commands. Generic create and
	// PATCH refuse them with 422; unknown names panic at mount. Unlike server-owned
	// fields, these remain writable through their owning command.
	Immutable []string

	// Present is how this resource reads: the words a person is shown, which field
	// names a row, which block a field belongs to, which fields a column may be
	// ordered by. Every part is optional and the zero value is every resource
	// written before it existed — the served catalogue prints no `presentation`
	// key for an entry nobody hinted, so no shell already installed reads a
	// different document. kit/rest refuses the declaration at mount, not the
	// request: a hint that names no field is a wiring mistake.
	Present entity.EntryHints

	// HookEvents names the events the hooks below publish. They are appended
	// to the create, update and delete operations' x-platformkit-events, so
	// the OpenAPI document says what a write can emit and kit/app's boot gate
	// sees it.
	//
	// It closes one direction and it is worth being plain about which. The
	// gate compares what the routes declare against what the manifests
	// declare; nothing reads what a handler actually publishes. So an event
	// named here and missing from a manifest fails startup, and an event a
	// hook publishes that nobody named here is still invisible to everything
	// but the outbox.
	// Deprecated: use HookTopics when the payload type is known.
	HookEvents []string

	// HookTopics declares hook events with their payload types for route metadata.
	HookTopics []events.Declared

	// Gate optionally decides whether the current caller may directly change these
	// fields after locking and merging, before writing. JSON and in-process writes
	// share it. It supplements authorization and requires an entity revision field;
	// see gate.go for the write contract.
	Gate Gate

	// The hooks run inside the request's transaction, after the write and
	// after the event, so a hook can publish more events or write more rows and
	// all of it commits together.
	//
	// There is no AfterUpdate, and the absence is a decision rather than an
	// omission: nothing in three repositories ever set one, and a hook nobody
	// writes is a parameter every reader of this struct has to rule out. A
	// module that needs one adds it back in the commit that uses it. Its other
	// half — the question asked before a write rather than after — is Gate, above,
	// which is the hook this repository was missing.
	AfterCreate func(ctx context.Context, tx db.Tx[db.Tenant], e T) error

	// Create makes the entity's owner the one creator: when set, the JSON create,
	// the generated form and CreateRow hand the decoded body to it and return what
	// it answers, and they run none of their own create steps — no identity, no
	// file filing, no version, no event and no AfterCreate. The owner publishes in
	// the transaction it is given. Mount refuses it beside History, whose first
	// version would never be recorded.
	Create      func(ctx context.Context, tx db.Tx[db.Tenant], e T) (T, error)
	AfterDelete func(ctx context.Context, tx db.Tx[db.Tenant], e T) error

	// Audience computes the richtext record's file visibility before filing uses.
	// Nil keeps uses private; errors refuse the write. It runs once on the current
	// entity in the write transaction so shared files need no later lock upgrade.
	Audience func(ctx context.Context, tx db.Tx[db.Tenant], e T) (bool, error)
}

// The five operations a Spec may offer, spelled the way the routes that mount
// them are spelled. The values are httpx.CRUD — the same strings the operation
// ids end with and the keys of the `writes` map near the bottom of this file —
// so one verb means the same thing in the route table, on a screen and in the
// catalogue a phone parses.
const (
	List   = httpx.CRUDList
	Read   = httpx.CRUDRead // a constant, not the field: Spec.Read is a permission string
	Create = httpx.CRUDCreate
	Update = httpx.CRUDUpdate
	Delete = httpx.CRUDDelete
)

// The three events every Spec publishes.
const (
	Created = "created"
	Updated = "updated"
	Deleted = "deleted"
)

// Event is the full name of one of this Spec's events, "<module>.<entity>.<verb>".
// A module lists these in its manifest's Declared, and kit/app refuses to start
// when a Spec would publish one that nothing declared.
//
// It refuses a Spec that names no Module, because the manifest that calls it is
// built before any router exists: the kernel fills an empty Module at Mount (see
// Spec.Mount), and an event name formed from what the kernel will later fill is a
// promise no manifest could make — the name would read ".task.created", and the
// app gate would refuse a route that published the real one.
func (s Spec[T]) Event(verb string) string {
	if s.Module == "" {
		panic(fmt.Sprintf("rest: the Spec for %q builds the name of its %q event and names no Module; leave Module empty only where a router mounts the Spec, which fills it — Event, Events and Declared need the module the events belong to", s.Entity, verb))
	}
	return s.Module + "." + s.Entity + "." + verb
}

// Events are the three names this Spec publishes, so a manifest can name them
// without spelling them.
func (s Spec[T]) Events() []string {
	if s.input != nil {
		return nil
	}
	return []string{s.Event(Created), s.Event(Updated), s.Event(Deleted)}
}

// Declared is the same three names, each with T as its payload type: what
// emit marshals is the entity, so the manifest's promise about a Spec's event is
// the entity type and nothing else. A module that mounts a Spec puts this in
// its manifest rather than writing the three types out beside the three names.
func (s Spec[T]) Declared() []events.Declared {
	topics := s.Topics()
	if topics.Created.Name() == "" {
		return nil
	}
	return []events.Declared{topics.Created.Declaration(), topics.Updated.Declaration(), topics.Deleted.Declaration()}
}

// Topics returns this Spec's generated CRUD topics in created, updated, deleted order.
func (s Spec[T]) Topics() Topics[T] {
	if s.input != nil {
		return Topics[T]{}
	}
	return Topics[T]{
		Created: events.DeclareTopic[T](s.Event(Created)),
		Updated: events.DeclareTopic[T](s.Event(Updated)),
		Deleted: events.DeclareTopic[T](s.Event(Deleted)),
	}
}

// Topics contains the generated CRUD events for one entity type.
type Topics[T any] struct {
	Created events.Topic[T]
	Updated events.Topic[T]
	Deleted events.Topic[T]
}

// Schema describes the entity to anything that did not compile against it: the
// generated screens of stage E4, and the list operation's own documentation.
func (s Spec[T]) Schema() crud.Schema {
	if s.input != nil {
		return crud.Schema{Module: s.Module, Entity: s.Entity, Path: s.Path, Fields: s.input.fields}
	}
	return crud.Schema{Module: s.Module, Entity: s.Entity, Path: s.Path, Fields: s.choiceFields()}
}
