package internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// TaskForUpdate is contracts.LockedReader: the read a diff is made against, with
// the row locked so the revision next to the document is the revision nobody else
// can move while this transaction is open.
//
// It is a method on the same Service rather than a second object because there is
// one implementation of "read a task" in this module, and two objects would be two
// places to look for it — which is how the plain read and the locked read start to
// disagree. modules/site's LockedReader is the same shape for the same reason.
func (s *Service) TaskForUpdate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Task, error) {
	return crud.GetForUpdate[*contracts.Task](tx, id)
}

var (
	_ contracts.LockedReader = (*Service)(nil)
	_ contracts.Writer       = (*Service)(nil)
)

// Save is contracts.Writer: the whole-row write an approved proposal comes
// through, on the fields a proposal is allowed to carry.
//
// Three things happen in this order, and the order is the whole method. The row is
// locked, because a check against a row somebody else can still write is not a
// check. The merged document is compared against that locked row and refused if it
// moves a field a command owns — assigneeId, slaBreached, resolvedAt, resolution,
// or status — because those move a state machine and publish an event of their own,
// and a proposal that set one alone would change the fact and tell nobody. Then the
// proposal's own fields are written, the row's revision moves in the same statement,
// and task.task.updated is published beside it, so an applied change is in the trail
// the way any other write of the row is.
//
// This is the one door that writes a protected field without the gate in front of
// it, and that is the decision rather than an oversight: a gate that stood here as
// well would refuse the change control's own apply, and the proposal could be
// approved and never applied — a wall, not a door. What makes it safe is that it is
// single, named in one place, and audited: modules/site's settings gate states the
// same rule about its own Save.
func (s *Service) Save(ctx context.Context, tx db.Tx[db.Tenant], in *contracts.Task) (*contracts.Task, error) {
	if in == nil {
		return nil, fmt.Errorf("%w: there is no task to write", crud.ErrInvalid)
	}
	current, err := crud.GetForUpdate[*contracts.Task](tx, in.ID)
	if err != nil {
		return nil, err
	}
	if err := refuseUnproposableMove(current, in); err != nil {
		return nil, err
	}
	// The columns a proposal may move, and nothing else: a merged document that
	// gained a field nobody proposed would be written here if the list came from the
	// document instead of from the module. The revision moves with the data, in the
	// one UPDATE, so there is no moment where the number and the values disagree.
	columns := append(proposableColumns(), "revision", "updated_at")
	in.Revision = current.Revision + 1
	if err := crud.Update(ctx, tx, in, columns...); err != nil {
		return nil, err
	}
	return in, events.Publish(ctx, tx, contracts.EventUpdated, in)
}

// refuseUnproposableMove names the first field a merged document moves that no
// proposal is allowed to move, and "" is the answer that lets the write through.
//
// The comparison is against the locked row and not against the request: a merged
// document carries the whole task, every unchanged field in it as well, and a rule
// that refused a document for *carrying* assigneeId would refuse every apply of
// every proposal ever made. A field a proposal leaves alone arrives as the value
// that is already there.
func refuseUnproposableMove(current, in *contracts.Task) error {
	// The four command-owned fields and the status, named here in terms of the two
	// lists the contracts package publishes, so the sentence a refusal quotes and the
	// list rest.Spec's Immutable quotes are the same four names (and the same one
	// status) rather than a third spelling of them.
	if current.Status != in.Status {
		return unproposable("status", "Assign and Resolve move the lifecycle, and publish task.assigned and task.resolved beside it")
	}
	for _, name := range contracts.CommandOwnedFields {
		moved := false
		switch name {
		case "assigneeId":
			moved = (current.AssigneeID == nil) != (in.AssigneeID == nil) ||
				(current.AssigneeID != nil && in.AssigneeID != nil && *current.AssigneeID != *in.AssigneeID)
		case "slaBreached":
			moved = current.SLABreached != in.SLABreached
		case "resolvedAt":
			moved = (current.ResolvedAt == nil) != (in.ResolvedAt == nil) ||
				(current.ResolvedAt != nil && in.ResolvedAt != nil && !current.ResolvedAt.Equal(*in.ResolvedAt))
		case "resolution":
			moved = current.Resolution != in.Resolution
		}
		if moved {
			return unproposable(name, "a command of its own writes it, with the state change and the event that belong to it")
		}
	}
	return nil
}

func unproposable(field, why string) error {
	return fmt.Errorf("%w: %s belongs to a command of its own — %s — so no proposal moves it", crud.ErrInvalid, field, why)
}

// proposableColumns turns the module's own field vocabulary into the columns behind
// it, through the schema rather than by a second list of names: the list a reviewer
// reads, the list the gate answers from and the list this write uses are then one
// list, spelled once, and a rename in the struct moves all three.
func proposableColumns() []string {
	fields := crud.Fields[*contracts.Task]()
	out := make([]string, 0, len(contracts.ProtectableFields))
	for _, name := range contracts.ProtectableFields {
		f, ok := crud.FieldNamed(fields, name)
		if !ok {
			// A vocabulary that names no field is a mount-time bug, like every other
			// misspelled field name in this kernel: the refusal built on that name
			// would be a sentence about a field that does not exist.
			panic("task: ProtectableFields names " + name + ", which is not a field of a task")
		}
		out = append(out, f.Column)
	}
	slices.Sort(out) // a stable column list, so a generated statement is one statement
	return out
}
