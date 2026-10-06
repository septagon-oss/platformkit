package rest

// This file is the door in front of a write: the question a Spec asks once the
// row is locked and the body merged, and the one computation every door uses to
// say what a write changes.
//
// It exists because a module could not otherwise answer "may this field be
// written directly" at all. The generic PATCH route is mounted from module code
// that never sees the merge, so a gate that stood in each module's own service —
// which is what modules/site had to do, on its singleton's Save closure — covers
// one door per module and none of the ones a Spec mounts. The answer had to be in
// the orchestration JSON routes and in-process resources share, which is the
// sentence in rest.go above createRow.
//
// Two rules make it a door and not a wall:
//
//   - The gate is asked about the *entity*, not the request. Changed is computed
//     from the value the merge produced, so a body that resends the current value
//     is not a change and is not refused. A gate that read the request's keys
//     would refuse a form that redraws the whole record.
//   - The gate never stands on the write an approved change comes through. It
//     stands on the doors a person types into. A gate on both is a wall: the
//     change could be proposed and never applied.

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// The three verbs a door writes with. They are the Spec's own operations, restated
// as plain strings so a gate — which lives below this package's huma graph in
// every module's build — can branch on a write without importing it.
const (
	VerbCreate  = "create"
	VerbUpdate  = "update"
	VerbDelete  = "delete"
	VerbCommand = "command"
)

// RevisionField is the json name a Spec looks for to find the entity's own write
// counter, and the name every gate refusal quotes. An entity grows the field by
// writing it in its own struct, the way site settings and tasks both did: a number
// is the only thing a reviewer of a diff can be told, and the only thing a stale
// proposal can be refused with.
//
// The name itself lives in kit/entity, because the question it answers — is this
// field the server's — is asked by the form, the merge and this file, and a second
// spelling of it would be a second thing to keep honest.
const RevisionField = entity.RevisionField

// Gate is asked after the row is locked and the body merged, and before anything
// is written: this row, these fields, now.
//
// It refuses with an error the caller can act on and nothing below this package
// interprets that error: a gate that returns crud.ErrConflict gets 409 the way
// every other write here does, and a gate that returns a problem document gets
// that document. A non-nil error means nothing was written, nothing was published
// and no hook ran.
//
// It is not an authorization decision. Authorization answers whether this caller
// may write at all; a gate answers whether this write has to go through a proposal
// first, whichever caller it is — which is why it is asked after the permission
// guard and after crud.RecheckTenant, and why a composition wires one to
// modules/change and this package knows nothing about it.
type Gate interface {
	Check(ctx context.Context, tx db.Tx[db.Tenant], w Write) error
}

// Write is one write the gate is being asked about, as the door describes it.
//
// Module and Entity come from the Spec and never from the request, so a caller
// cannot name a subject whose answer they prefer; Changed comes from the merged
// entity and never from the request's key list, for the reason in this file's
// header. Everything else the gate could ask for — the row's values, the actor,
// the tenant — it reads through the transaction and the context it is handed.
type Write struct {
	Module  string
	Entity  string
	ID      uuid.UUID // the row the kernel locked; uuid.Nil for a create that has no id yet
	Verb    string    // one of VerbCreate, VerbUpdate, VerbDelete, VerbCommand
	Command string    // "" unless Verb is VerbCommand
	Changed []string  // json field names, in schema order
}

// changedNames answers the one question every door asks the same way: which of
// this entity's own fields does the write leave different?
//
// It compares canonical JSON per field, through the entity's own tags, so the name
// in the answer is the name a screen, a PATCH body and the OpenAPI document all
// use — there is no second vocabulary to drift. Read-only fields are not asked
// about: the server owns those at every door, and a field a person cannot write is
// not a field they need a proposal for.
//
// before is the encoding taken before the merge (jsonSnapshot), and after is the
// entity as the merge and its own Validate left it.
func changedNames(fields []crud.Field, before map[string]json.RawMessage, after any) ([]string, error) {
	now, err := jsonSnapshot(after)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fields {
		if entity.ServerOwned(f) {
			continue
		}
		got, had := now[f.Name], before[f.Name]
		if string(got) == string(had) {
			continue
		}
		out = append(out, f.Name)
	}
	return out, nil
}

// jsonSnapshot encodes an entity as its top-level fields, each left as the bytes
// the encoder made. UseNumber is not needed because nothing here reads a number
// back: the comparison is byte-for-byte, which is also how change control digests
// the exact reviewed bytes (changecontracts.Diff).
func jsonSnapshot(e any) (map[string]json.RawMessage, error) {
	encoded, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("rest: the row a write is compared against cannot be written down: %w", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("rest: the row a write is compared against is not a JSON object: %w", err)
	}
	return out, nil
}

// revisionColumn names the column a Spec with this entity has to write to move the
// row's revision, and "" when the entity carries no such field. It asks the schema
// rather than the struct because the schema is what the rest of this package reads.
//
// The field has to be the server's own — an int named revision, which is what
// entity.ServerOwned asks — because a Spec that carries a caller-writable field of
// that name is not carrying a write counter, and moving it would be moving a value
// somebody else owns.
func revisionColumn(fields []crud.Field) string {
	f, ok := crud.FieldNamed(fields, RevisionField)
	if !ok || f.Type != crud.TypeInt || !entity.ServerOwned(f) {
		return ""
	}
	return f.Column
}

// noEntity reports a nil entity handed to a kernel rule about one. T is a pointer
// type, so this is reachable — a request with no body decodes to one — and == does not
// compile against a type parameter, which is why every rule below asks it this way.
func noEntity(e any) bool {
	v := reflect.ValueOf(e)
	return !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil())
}

// revisionValue is the entity's own counter, through the schema's index.
func revisionValue(e any, f crud.Field) int64 {
	return reflect.ValueOf(e).Elem().FieldByIndex(f.Index).Int()
}

// clearRevision takes the counter out of an entity a create is about to insert.
//
// crud.Reset already discards whatever arrived for Base's four, on the reasoning
// quoted above createRow: the server owns those outright, and a caller sending an id
// is not reaching for a door of its own. The write counter is the same kind of field
// — it is the number of writes this row has had, which a create cannot know before
// it has happened — and leaving it writable made a row arrive at whatever number its
// first caller chose, which is the number every later stale-base refusal quotes.
//
// Zero is the value that means "ask the column", the same one every other default
// takes: the entity's own `default:1` answers, and the row starts where the
// migration says it starts.
func (s Spec[T]) clearRevision(fields []crud.Field, e T) {
	column := revisionColumn(fields)
	if column == "" || noEntity(e) {
		return
	}
	f, _ := crud.FieldNamed(fields, RevisionField)
	reflect.ValueOf(e).Elem().FieldByIndex(f.Index).SetInt(0)
}

// blankRow is what a row that holds nothing anybody chose would hold: an empty entity,
// with every field that has a declared default holding that default.
//
// The delete door measures against this and not against nothing. Every value the row held
// does go, which is why a delete is the largest write there is — but a value the entity
// starts at is one nobody chose, and a refusal that named it would send a person to empty
// a field that refills itself the moment they do. A way through that closes itself is the
// wall this file's own invariant refuses, and the delete is the one verb with no proposal
// to walk through: change control moves values onto a row, it does not remove one.
//
// The defaults come from the schema rather than from the entity's Validate, because
// Validate is allowed to refuse a blank row before it has defaulted anything (a task has
// no title), and the answer this comparison wants is what the column starts at, which is
// what `default:"open"` and the migration's DEFAULT both say.
func (s Spec[T]) blankRow() map[string]json.RawMessage {
	out := s.zeroSnapshot()
	if out == nil {
		return nil
	}
	for _, f := range crud.Fields[T]() {
		if f.Default == "" || f.ReadOnly {
			continue
		}
		value, err := coerce(f, f.Default)
		if err != nil {
			// A default the schema's own decoder refuses is a schema bug, and the
			// comparison is better off without that one field than off the whole
			// baseline: every other field still says what the row was given.
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			continue
		}
		out[f.Name] = encoded
	}
	return out
}

// entityValidate is kit/crud's own optional check, asked here and not only inside
// crud.Update, so an invalid body is refused before the caller is told which of
// their fields is protected. crud.Update asks it again on the way to the write;
// that is why a Validator has to be idempotent, which every entity behind a Gate
// is (they trim, default and range-check; none of them counts anything).
func entityValidate(ctx context.Context, e any) error {
	v, ok := e.(crud.Validator)
	if !ok {
		return nil
	}
	if err := v.Validate(ctx); err != nil {
		return fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	return nil
}

// gate asks this Spec's door about one write, if the composition gave it one.
//
// A CRUD write that changes nothing is not asked about: it writes nothing, and
// there is no field to name in a refusal. A command is asked whatever it changes,
// because the question about a command is not "which of these fields moved" — the
// kernel does not know, and neither does the body — but "this account is about to
// run Resolve", which the module can answer.
func (s Spec[T]) gate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, verb, command string, changed []string) error {
	if s.Gate == nil || (len(changed) == 0 && command == "") {
		return nil
	}
	return s.Gate.Check(ctx, tx, Write{
		Module: s.Module, Entity: s.Entity, ID: id,
		Verb: verb, Command: command, Changed: changed,
	})
}

// zeroSnapshot is the encoding of a row that has never been written: the base a
// create's Changed list is measured against. A field the body left alone is absent
// from both encodings and so changes nothing, which is why creating a task whose
// priority defaults to "normal" is not a write of the priority.
func (s Spec[T]) zeroSnapshot() map[string]json.RawMessage {
	var zero T
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Pointer {
		return nil
	}
	out, err := jsonSnapshot(reflect.New(t.Elem()).Interface())
	if err != nil {
		// A blank entity of a type whose populated form encodes is not a request-time
		// condition: it is a type that cannot be marshalled at all, which every door
		// below would fail on anyway.
		return nil
	}
	return out
}

// commandGate asks the Spec's door about a command, in the same transaction the
// command runs in — so the lock the answer is based on is the lock the command
// then takes, and a proposal applied between the two is a proposal that waits its
// turn rather than one that interleaves.
//
// The question carries the command's verb and no field list. Which fields a command
// moves is the subject owner's knowledge — resolution belongs to Resolve, which
// moves it along with the status and the event beside it — and the door asks for it
// by verb rather than holding a copy somebody has to remember to write: a command
// whose author forgot a declaration would stand ungated, which is the bypass a door
// exists to close. The answer a module gives for its own commands is normally empty
// (that is the difference between a door and a wall: a protected slaDeadline must
// never stop a task from being resolved), and an empty answer costs no switch read.
//
// A collection command is not asked: it names no row, and its answer would be a
// subject with nothing in it. A Spec with no gate is not asked either, which is what
// leaves every command written before this door behaving exactly as it did.
func (s Spec[T]) commandGate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, verb string, opts CommandOptions) error {
	if s.Gate == nil || opts.Collection {
		return nil
	}
	if _, err := crud.GetForUpdate[T](tx, id); err != nil {
		return err
	}
	return s.gate(ctx, tx, id, VerbCommand, verb, nil)
}

// gateFault names what a Spec with a door in front of its writes gets wrong, and
// "" when it is coherent. Like every other refusal in check, it is a mount-time
// panic: a wiring mistake, not a request-time condition.
func (s Spec[T]) gateFault() string {
	if s.Gate == nil {
		return ""
	}
	if revisionColumn(crud.Fields[T]()) == "" {
		return fmt.Sprintf("sets a Gate on an entity with no %q field: a write refused because the row may have moved has to be able to say what it moved to, and nothing else on the row counts writes", RevisionField)
	}
	if !s.offersWrite() {
		return "sets a Gate and mounts no write, so there is no door to answer at"
	}
	return ""
}

// bumpRevision moves the entity's own write counter and returns the columns to
// write, with the revision's column appended when the entity has one. It is the
// kernel's rule for a Spec and nobody else's: modules/site's singleton bumps its
// own number inside its own Save, and a kernel rule that fired there too would
// move it twice per write.
func (s Spec[T]) bumpRevision(fields []crud.Field, e T, write []string) []string {
	column := revisionColumn(fields)
	if column == "" {
		return write
	}
	f, _ := crud.FieldNamed(fields, RevisionField)
	value := reflect.ValueOf(e).Elem().FieldByIndex(f.Index)
	value.SetInt(value.Int() + 1)
	return append(write, column)
}

// revisionOf reads an entity's own counter, and 0 for an entity that carries no
// kernel write count. It is what a conditional write is compared against and what its
// ETag is made of, and there is exactly one reading of the number in this package.
func (s Spec[T]) revisionOf(fields []crud.Field, e T) int64 {
	column := revisionColumn(fields)
	if column == "" || noEntity(e) {
		return 0
	}
	f, _ := crud.FieldNamed(fields, RevisionField)
	return revisionValue(e, f)
}

// revisionTag is the response's ETag for a row the kernel counts writes on, and ""
// for every other entity. It quotes the number the row itself carries, which after a
// write is the one the write produced: a caller that read this response can quote it
// back and be refused if the row moved since.
func (s Spec[T]) revisionTag(fields []crud.Field, e T) string {
	if revisionColumn(fields) == "" || noEntity(e) {
		return ""
	}
	f, _ := crud.FieldNamed(fields, RevisionField)
	return `"` + strconv.FormatInt(revisionValue(e, f), 10) + `"`
}
