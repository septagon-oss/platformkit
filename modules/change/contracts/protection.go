package contracts

// This file is the one extension point that makes a field protected, and it is a
// question rather than a policy. A *protected field* is a field a person may not
// write directly at this installation: the write has to be put forward as a
// proposal and applied by a different account. Which fields those are is a fact
// about the installation and the entity, so this module asks the question at the
// doors it can reach and answers nothing about it: it owns no switch, no
// permission and no sentence about anybody's fields.
//
// The question is asked with field names and answered with field names, because
// the two answers a person can act on are "nothing here needs a proposal, write
// it" and "these two fields do, and the door is over there". An answer that named
// only the entity would send a caller to propose a change that is refused for the
// same reason — which is why Refusal carries Fields.

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The four verbs a door writes with, spelled the way the routes that use them are
// spelled. A gate that could not tell a delete from a patch could not answer the
// first question anybody asks of a protected row — "may I remove it?" — because a
// delete changes no field the caller sent: the row is what goes.
const (
	WriteCreate  = "create"
	WriteUpdate  = "update"
	WriteDelete  = "delete"
	WriteCommand = "command"
)

// SubjectRef names the row a write is about, in the terms a gate reads. It is a
// value and not a port: the door already knows the triple, because its own Spec
// or binding wrote it, and a gate never trusts a caller to name somebody else's
// subject and take that subject's answer (Gate.Check says what happens then).
type SubjectRef struct {
	Module string
	Entity string
	// ID is uuid.Nil for an entity with one row per tenant, which is what
	// Proposal.SubjectID means by the same value.
	ID uuid.UUID
}

// DirectWrite is one write a person is about to make, as the door describes it.
//
// Changed is computed by the kernel from the entity and never taken from the
// request: the keys a body happened to carry are not the question ("did the value
// move"), and the same rule has to answer at a JSON route, at a generated screen's
// form, at a command and at a job that calls the same write beneath the HTTP. That
// is what makes one answer cover every door a request can reach rather than one
// answer per transport.
type DirectWrite struct {
	Subject SubjectRef
	// Verb is one of the four above.
	Verb string
	// Command is the command's own verb ("resolve") and empty for the three CRUD
	// verbs. It is here because a field's owner is often the command: a rule that
	// protected resolution would refuse Resolve, which is the only door that moves
	// the field along with the status and the event that belong to it.
	//
	// For a command's write it is also the whole question. kit/rest cannot know
	// which fields a command moves without running it, and running it is the
	// write, so it asks about every command on a gated Spec and names no fields
	// here; the module that owns the row knows what its own commands move, and this
	// port is where that knowledge answers. A list copied onto the command would be
	// a second, editable spelling of the same fact, and one whose omission silently
	// ungated its own door.
	Command string
	// Changed holds the entity's json field names whose value this write moves,
	// in the entity's own schema order. Empty means either that the write changes
	// nothing the entity carries — a body that resends current values — or that the
	// door cannot name them, which is a command's case and the reason Command is.
	Changed []string
}

// Protection is the extension point a module's vocabulary answers through. It is
// a subset question and nothing else: of the fields this write would change, which
// may not be written without a proposal?
//
// It is answered by the composition over the subject owner's published field
// vocabulary, because the answer needs two facts that live in different places —
// which fields this entity protects, which only the module that owns the row
// knows, and whether this installation has change control switched on for it,
// which only the installation knows. A `modules/task` that answered it alone would
// need the installation's switch; a switch that answered it alone would need
// somebody's field names.
//
// The empty answer is the common one and costs no flag read. A nil Protection at a
// Gate means nothing is protected anywhere, which is what every installation that
// has not decided otherwise means.
type Protection interface {
	Protected(ctx context.Context, tx db.Tx[db.Tenant], w DirectWrite) ([]string, error)
}
