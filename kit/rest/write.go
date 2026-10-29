package rest

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// ErrStaleTag is a conditional write whose tag the row has passed: the caller read it
// at one revision and is writing after somebody else moved it. kit/rest's Fault turns
// it into the 412 that says so, with the revision the row is on now in the sentence.
var ErrStaleTag = errors.New("rest: the row moved since this write's tag was read")

// JSON routes and in-process resources share their write orchestration.
func (s Spec[T]) createRow(ctx context.Context, tx db.Tx[db.Tenant], e T) (T, error) {
	if s.Session != nil {
		var zero T
		return zero, crud.ErrInvalid
	}
	var err error
	e, err = s.choiceCreateRow(e)
	if err != nil {
		return e, err
	}
	if s.Create != nil {
		return s.Create(ctx, tx, e)
	}
	crud.Reset(e)                        // IDs, tenancy and timestamps belong to the server at both doors.
	s.clearRevision(crud.Fields[T](), e) // and so does the count of this row's writes
	// The record has to be itself before its own body can be filed. What a body
	// shows is recorded in prepareFiles below, and a use names the record that
	// shows the file — an id stamped by crud.Create one step later would be filed
	// under nil, which is the row nobody can link back to or sweep forward. So
	// the id is chosen here, and Create keeps the id it finds rather than making
	// another one. Reset already dereferenced the entity, so there is nothing
	// between these two lines that a nil body could have reached.
	entity.BaseOf(e).ID = uuid.New()
	if err := s.prepareFiles(ctx, tx, e, nil); err != nil {
		return e, err
	}
	// The trail's field is discarded here for the same reason the id is chosen here:
	// the caller does not get to write it. A create replaces no row, so there is no
	// before half for anybody to supply, and a body that named one was writing the
	// history of a save that never happened. The create door decodes straight into
	// the entity, so hidden:"true" buys it nothing — that tag is the REST document's,
	// and a document that does not offer a member is not a decoder that refuses one.
	// The patch needs no such line because merge reads the body as a map of schema
	// fields, and the diff is no field: it answers `{"changes":[…]}` with "there is no
	// field \"changes\"", which is the same refusal in the other door's words.
	discardDiff(e)
	// The create door is not gated, and the gate's own invariant is why: a proposal
	// is a diff against a row that has a revision, so there is no proposal to make
	// about a row that does not exist yet, and a gate here would be the only way
	// past a field it refuses — a wall. Protecting a record's protected fields means
	// guarding the writes that change a record somebody else may have already read:
	// the patch, the delete, and the commands that declare them.
	if err := crud.Create(ctx, tx, e); err != nil {
		return e, err
	}
	// The row's first version, at the number the row itself starts on. Nothing
	// preceded it, so it names no changed fields: a version's `changed` answers
	// "what did this write move", and the first write moved everything by being
	// the beginning rather than by moving anything.
	if s.History {
		snap, err := jsonSnapshot(e)
		if err != nil {
			return e, err
		}
		if err := s.recordVersion(ctx, tx, e, snap, nil); err != nil {
			return e, err
		}
	}
	return e, s.emit(ctx, tx, Created, e, s.AfterCreate)
}

// updateRow locks the row before merging and validating its submitted fields.
// Responses, hooks and events therefore use the snapshot that this transaction
// replaces, including after contention.
func (s Spec[T]) updateRowRaw(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, fields []crud.Field, values map[string]any, ifMatch string) (T, error) {
	if s.Session != nil {
		var zero T
		return zero, crud.ErrInvalid
	}
	e, err := crud.GetForUpdate[T](tx, id)
	if err != nil {
		return e, err
	}
	// Snapshot the locked row before merge so diffs describe the state this
	// transaction actually replaced.
	before := rowCopy(e)
	// The gate compares values, so resending an unchanged field is not a change.
	beforeJSON, err := jsonSnapshot(e)
	if err != nil {
		return e, err
	}
	values, err = s.choicePatch(tx, e, fields, values, ifMatch)
	if err != nil {
		return e, err
	}
	columns, err := merge(e, fields, s.Immutable, values)
	if err != nil {
		return e, err
	}
	// An empty patch performs no validation or write, but still rechecks tenant
	// ownership: a read-visible foreign row must not become a successful write.
	if len(columns) == 0 {
		if err := crud.RecheckTenant(tx, e); err != nil {
			return e, err
		}
		return e, nil
	}
	if err := s.validateUpdate(ctx, tx, id, e, fields, beforeJSON, ifMatch); err != nil {
		return e, err
	}
	if err := s.persistUpdate(ctx, tx, e, fields, columns); err != nil {
		return e, err
	}
	if err := s.outdateTranslations(ctx, tx, id, columns); err != nil {
		return e, err
	}
	// Diff the normalized persisted value so history and the event describe
	// the same update; discard the temporary diff after both are recorded.
	done, err := reportDiff(before, e, values)
	if err != nil {
		return e, err
	}
	defer done()
	if err := s.recordUpdateVersion(ctx, tx, e, fields, beforeJSON); err != nil {
		return e, err
	}
	return e, s.emit(ctx, tx, Updated, e, nil)
}

// rowCopy is the shallow copy of a locked row: the values it held when the transaction
// locked it, kept while the caller merges a body into the original. Shallow is enough
// because merge assigns whole fields through the entity's own reflect.Value and never
// writes through a pointer or extends a slice, so nothing a merge does to the original
// reaches the copy. It answers any, because the door that asks is generic over the
// entity and cannot name it; kit/events' ChangesOf takes the two rows as values and
// refuses a pair that turns out to be two different types.
func rowCopy[T any](e T) any {
	v := reflect.ValueOf(e)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil
	}
	c := reflect.New(v.Elem().Type())
	c.Elem().Set(v.Elem())
	return c.Interface()
}

func discardDiff(e any) {
	if rec, ok := e.(events.Recorder); ok {
		rec.SetChanges(nil)
	}
}

// reportDiff records sorted submitted-field changes from the locked snapshot to
// the normalized persisted row when e implements events.Recorder. Failure aborts
// the caller's transaction. The returned cleanup removes the temporary diff after
// history and publication so it cannot leak into the response.
func reportDiff(before any, e any, values map[string]any) (func(), error) {
	rec, ok := e.(events.Recorder)
	if !ok {
		return func() {}, nil
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	changes, err := events.ChangesOf(before, e, names...)
	if err != nil {
		return nil, err
	}
	rec.SetChanges(changes)
	return func() { rec.SetChanges(nil) }, nil
}

func (s Spec[T]) deleteRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, ifMatch string) (T, error) {
	e, err := crud.GetForUpdate[T](tx, id)
	if err != nil {
		return e, err
	}
	// Whose row it is, asked here for the same reason the update asks it, and with
	// more to lose: row-level security filters a DELETE by its USING clause alone
	// — there is no new row for a WITH CHECK to inspect — so on a table every
	// tenant may read, the statement below reaches a row the request's tenant may
	// not write, and the event would be published over a row it removed.
	if err := crud.RecheckTenant(tx, e); err != nil {
		return e, err
	}
	if err := refuseStaleTag(ifMatch, s.revisionOf(crud.Fields[T](), e)); err != nil {
		return e, err
	}
	if err := refuseStaleTag(ifMatch, s.revisionOf(crud.Fields[T](), e)); err != nil {
		return e, err
	}
	// A delete is the largest write there is: every value the row held goes, so the
	// fields it held are the fields it changes, and a protected value makes the
	// delete protected with it. It is the one verb whose own request says nothing —
	// there is no body to read a field list from — which is why the answer is
	// computed from the row instead, and why leaving this door out of a gate would
	// be a way past it that nobody had to write.
	//
	// The baseline is a blank row with its own defaults applied, and not nothing. A
	// value the entity defaults to is one nobody chose, and a refusal naming it would
	// send a person to empty a field that refills itself the moment they do — a way
	// through that closes itself is the wall this door's own invariant refuses.
	if s.Gate != nil {
		changed, err := changedNames(crud.Fields[T](), s.blankRow(), e)
		if err != nil {
			return e, err
		}
		if err := s.gate(ctx, tx, id, VerbDelete, "", changed); err != nil {
			return e, err
		}
	}
	// A record that stops existing shows nothing, so its uses end in the same
	// transaction that removes its row. A ledger row that outlived its record is a
	// file panel that links to a record no route answers, and — once the release
	// sweep exists — a file that can never be released because something that no
	// longer exists is said to be reading it.
	if err := s.clearFileUses(ctx, tx, e); err != nil {
		return e, err
	}
	if err := s.removeRow(ctx, tx, id); err != nil {
		return e, err
	}
	// The record is gone, so every translation of it is gone with it, in this
	// same transaction. No foreign key can span two modules' schemas — the rows
	// live in modules/translation's migration, not this one's — so the promise
	// this call is what keeps, and the alternative is a table of translations of
	// records that no longer exist, which no screen can name and no cleanup finds.
	if s.tracksTranslations() {
		if err := s.Translations.ForgetRecord(ctx, tx, s.Module, s.Entity, id); err != nil {
			return e, err
		}
	}
	return e, s.emit(ctx, tx, Deleted, e, s.AfterDelete)
}

// removeRow is the delete this resource declared: SoftDelete hides the row and
// keeps it for anything that referenced it, DeleteRow ends it. Spec's own flag
// decides which, and the two doors say it in their names.
func (s Spec[T]) removeRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) error {
	if s.SoftDelete {
		return crud.SoftDelete[T](ctx, tx, id)
	}
	return crud.DeleteRow[T](ctx, tx, id)
}

// CreateRow, UpdateRow and DeleteRow are this resource's write orchestration at
// the door an in-process caller stands at. The JSON routes and the generated
// screens go through the same three bodies; a seed writer, or any composition-time
// caller that is not a person's request, stands here. The reason they are
// exported and not copied is that the three things a write must never skip are
// inside those bodies — the row lock, the tenant recheck a DELETE cannot get from
// row-level security alone, and the event that announces the change — and a
// second spelling of them in an application is a spelling that can drift.
// Authorization is not one of them: a route asks httpx, a seed run asks its
// Authorizer, and neither answer belongs in this file.
func (s Spec[T]) CreateRow(ctx context.Context, tx db.Tx[db.Tenant], e T) (T, error) {
	if s.Session != nil {
		var none T
		return none, crud.ErrInvalid
	}
	if s.input != nil {
		var none T
		return none, problem.New(405, "Input-only resources have no writes")
	}
	return s.createRow(ctx, tx, e)
}

// UpdateRow patches the columns `values` names, of this Spec's own schema, and
// is the one door that keeps a caller from writing a field the Spec declared
// immutable: the check is inside the merge those columns go through. It quotes no
// revision tag, and cannot: the caller read this row inside the same transaction
// and holds its lock, where a person's form read one somewhere earlier — which is
// the staleness If-Match answers and the reason the JSON route hands a tag through.
// This Spec's Gate stands here all the same, because "may these fields be written
// directly" does not ask who is asking.
func (s Spec[T]) UpdateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, values map[string]any) (T, error) {
	if s.Session != nil {
		var none T
		return none, crud.ErrInvalid
	}
	if s.input != nil {
		var none T
		return none, problem.New(405, "Input-only resources have no writes")
	}
	return s.updateRow(ctx, tx, id, s.choiceFields(), values, "")
}

func (s Spec[T]) DeleteRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	if s.Session != nil {
		var none T
		return none, crud.ErrInvalid
	}
	if s.input != nil {
		var none T
		return none, problem.New(405, "Input-only resources have no writes")
	}
	return s.deleteRow(ctx, tx, id, "")
}

// emit publishes the event for a write and then runs the module's hook, both
// inside the request's transaction: an event that describes a change that
// rolled back is never seen, because it rolled back too.
func (s Spec[T]) emit(ctx context.Context, tx db.Tx[db.Tenant], verb string, e T, hook func(context.Context, db.Tx[db.Tenant], T) error) error {
	if err := events.Succeed(ctx, tx, db.Now()); err != nil {
		return err
	}
	if err := events.Publish(ctx, tx, s.Event(verb), e); err != nil {
		return err
	}
	if hook == nil {
		return nil
	}
	return hook(ctx, tx, e)
}

func (s Spec[T]) validateUpdate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, e T, fields []crud.Field, beforeJSON map[string]json.RawMessage, ifMatch string) error {
	// Preserve refusal order: validation, tenant ownership, revision, then gate.
	if err := entityValidate(ctx, e); err != nil {
		return err
	}
	if err := crud.RecheckTenant(tx, e); err != nil {
		return err
	}
	// Check tenant ownership before the tag so a stale-tag refusal cannot
	// disclose a foreign row's revision.
	if err := refuseStaleTag(ifMatch, s.revisionOf(fields, e)); err != nil {
		return err
	}
	changed, err := changedNames(fields, beforeJSON, e)
	if err != nil {
		return err
	}
	if err := s.gate(ctx, tx, id, VerbUpdate, "", changed); err != nil {
		return err
	}
	return nil
}

func (s Spec[T]) persistUpdate(ctx context.Context, tx db.Tx[db.Tenant], e T, fields []crud.Field, columns []string) error {
	if err := s.prepareFiles(ctx, tx, e, columns); err != nil {
		return err
	}
	// Update submitted columns, timestamp and revision together; untouched
	// columns retain the committed values validation and events read.
	write := append(append([]string{}, columns...), "updated_at")
	if err := crud.Update(ctx, tx, e, s.bumpRevision(fields, e, write)...); err != nil {
		return err
	}
	return nil
}

func (s Spec[T]) outdateTranslations(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, columns []string) error {
	// Mark translations outdated in the same transaction as their source write.
	if s.tracksTranslations() {
		for _, field := range translatableFields[T]() {
			if !slices.Contains(columns, field.Column) {
				continue
			}
			if err := s.Translations.MarkOutdated(ctx, tx, s.Module, s.Entity, field.Name, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s Spec[T]) recordUpdateVersion(ctx context.Context, tx db.Tx[db.Tenant], e T, fields []crud.Field, beforeJSON map[string]json.RawMessage) error {
	// Record the normalized snapshot before publishing its event, in the same
	// transaction. jsonSnapshot excludes the temporary event diff.
	if s.History {
		snap, err := jsonSnapshot(e)
		if err != nil {
			return err
		}
		if err := s.recordVersion(ctx, tx, e, snap, changedNamesFrom(fields, beforeJSON, snap)); err != nil {
			return err
		}
	}
	return nil
}
