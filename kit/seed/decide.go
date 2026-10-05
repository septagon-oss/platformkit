package seed

import (
	"maps"
	"reflect"
	"slices"

	"github.com/google/uuid"
)

// Snapshot is the owner's canonical state of seed-managed values. A writer
// excludes timestamps, server fields and unrelated human-managed values.
type Snapshot struct {
	Present  bool
	ID       uuid.UUID
	Fields   map[string]any
	Commands map[string]any
}

// Target is the canonical state requested by the embedded record after the
// owner has normalized its fields, resolved references and dates, and checked
// the values it accepts.
//
// CreateOnly carries the declared values that are an instruction for the
// record's creation and not a state to reconcile afterwards. A relative date is
// the case: `+3d` names three days from the run that writes the row, so its
// declared value moves with every clock, and the deadline itself belongs to
// whoever holds the task once it exists. Decide never reads these, so a rerun of
// the same file reads as unchanged; a writer applies them in Create and must not
// apply them in Update. The alternative is a value that moves with the clock and
// so reports an update on every run forever — a seed that rewrote, on every
// deploy, a field a person can change through its own screen. A file record's
// `asset` bytes are the other case: an upload writes them once and no rerun puts
// new bytes behind the same row, so they instruct the record's creation and are
// never a state to reconcile against what the store holds.
type Target struct {
	Fields     map[string]any
	Commands   map[string]any
	CreateOnly map[string]any
}

// Action describes the write that the owner needs to perform.
type Action string

const (
	Create    Action = "create"
	Update    Action = "update"
	Unchanged Action = "unchanged"
)

// Decision names only changed managed fields and commands, never their values.
type Decision struct {
	Action  Action
	Changed []string
}

// unappliedField names the first field a record declares that its writer's
// Target carried nowhere: not into the state Decide reconciles, not into the
// create-only instruction, and not into a command. A field like that is a
// declaration the run read and then threw away — it would commit the record's
// other fields, map the key and publish the owner's event as though the file had
// been honoured. So it is refused at the field's own line, before any write, in
// the same shape as the command a writer does not offer is refused (house rule 7:
// a field nothing reads is not written). A writer accounts for a declared field
// by naming it in its Target: in Fields when the value is state the owner keeps,
// in CreateOnly when it is an instruction for the record's creation only.
func unappliedField(record Record, target Target) (string, Source) {
	for _, name := range slices.Sorted(maps.Keys(record.Fields)) {
		if _, reconciled := target.Fields[name]; reconciled {
			continue
		}
		if _, created := target.CreateOnly[name]; created {
			continue
		}
		if _, commanded := target.Commands[name]; commanded {
			continue
		}
		where, noted := record.Values["fields/"+name]
		if !noted || where.File == "" {
			where = record.Source
		}
		return name, where
	}
	return "", Source{}
}

// Decide is the shared comparison for fake and database writers. Writers must
// supply canonical values of the same Go types on both sides; the seeder never
// compares YAML bytes, password hashes or owner timestamps.
func Decide(current Snapshot, target Target) Decision {
	if !current.Present {
		return Decision{Action: Create}
	}
	var changed []string
	for _, name := range slices.Sorted(maps.Keys(target.Fields)) {
		if !reflect.DeepEqual(current.Fields[name], target.Fields[name]) {
			changed = append(changed, "fields/"+name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(target.Commands)) {
		if !reflect.DeepEqual(current.Commands[name], target.Commands[name]) {
			changed = append(changed, "commands/"+name)
		}
	}
	if len(changed) == 0 {
		return Decision{Action: Unchanged}
	}
	return Decision{Action: Update, Changed: changed}
}
