// Package contracts is everything another module, an app or a test may know
// about tasks: the entity, the events, the permissions and the Service
// interface. The implementation is in ../internal, which nothing outside this
// module can import, so a consumer that compiles took a capability rather than
// an implementation.
//
// A task is an assignable, SLA-tracked unit of work. Its point is the SLA: the
// entity exists to make "resolve within N hours or escalate" an auditable fact
// rather than a report somebody runs.
package contracts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/task/domain"
)

// The lifecycle: a task opens, is acknowledged when somebody takes it, is
// resolved when the loop closes, and is closed when nothing more will happen.
// Nothing may be assigned once it is resolved or closed.
const (
	StatusOpen         = domain.StatusOpen
	StatusAcknowledged = domain.StatusAcknowledged
	StatusInProgress   = domain.StatusInProgress
	StatusResolved     = domain.StatusResolved
	StatusClosed       = domain.StatusClosed
)

// The priorities, which order a queue and drive escalation.
const (
	PriorityLow      = "low"
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

// The two closed sets, spelled here as well as in the enum tags above: a tag
// is what the schema and the OpenAPI document read, and this is what Validate
// reads. Nothing can derive one from the other, so they are next to each other.
var (
	statuses   = []string{StatusOpen, StatusAcknowledged, StatusInProgress, StatusResolved, StatusClosed}
	priorities = []string{PriorityLow, PriorityNormal, PriorityHigh, PriorityCritical}
)

// Task is an assignable work item with a priority, a soft due date and a hard
// SLA deadline.
//
// The struct is the whole surface: the json tags are the API, the gorm tags are
// the table, the enum and validate tags are the schema a generated screen reads
// (kit/crud.Schema), and crud.Base contributes the id, the timestamps, the soft
// delete and the tenant column that row-level security matches on.
type Task struct {
	crud.Base

	// Title is the one-line summary. It is what a list screen shows.
	Title string `json:"title" gorm:"type:varchar(200);not null" validate:"required" doc:"Short summary of the task" minLength:"1" maxLength:"200" example:"Chiller-2 supply temperature out of band"`
	// Description is the optional long form.
	Description string `json:"description,omitempty" gorm:"type:text" ui:"widget:textarea;hide:list" doc:"Detailed description of the task"`

	// Status and Priority are closed sets; the enum tag is what a form renders
	// as a select and what Validate refuses a value outside.
	//
	// Both carry required:"false", and so does SLABreached below. huma reads a
	// struct field with no omitempty as a required request property, so without
	// it a create would have to send the three fields the server sets itself —
	// which is the same reasoning as crud.Base's, one level out.
	Status   string `json:"status" gorm:"type:varchar(20);not null;default:'open'" enum:"open,acknowledged,in_progress,resolved,closed" ui:"widget:select" doc:"Lifecycle state" default:"open" required:"false"`
	Priority string `json:"priority" gorm:"type:varchar(20);not null;default:'normal'" enum:"low,normal,high,critical" ui:"widget:select" doc:"Task priority" default:"normal" required:"false"`

	// Source and SourceRef link a task back to whatever raised it — a sensor, a
	// form — without a foreign key into another module's table. That is what
	// "cross-module dependencies are Go interfaces" costs at the database, and
	// it is cheaper than the alternative.
	Source    string `json:"source,omitempty" gorm:"type:varchar(40)" ui:"hide:list" doc:"Origin of the task" example:"sensor"`
	SourceRef string `json:"sourceRef,omitempty" gorm:"type:varchar(120)" ui:"hide:list" doc:"Reference into the source system" example:"asset:chiller-2"`

	// AssigneeID is who is responsible, nil while nobody is. Service.Assign
	// sets it, not a PATCH: assignment moves the status and publishes.
	AssigneeID *uuid.UUID `json:"assigneeId,omitempty" gorm:"type:uuid" ui:"widget:entity-picker" doc:"User responsible for the task" format:"uuid"`

	// DueAt is the soft target and SLADeadline the contractual one: two fields,
	// because "due soon" and "the promise is broken" are different things to
	// show, and only the second is what SLABreached is measured against.
	DueAt       *time.Time `json:"dueAt,omitempty" gorm:"type:timestamptz" ui:"widget:datetime" doc:"Soft target completion time"`
	SLADeadline *time.Time `json:"slaDeadline,omitempty" gorm:"type:timestamptz" ui:"widget:datetime" doc:"Hard SLA deadline; a breach is measured against this"`
	// SLABreached is stored rather than derived, so that a breach stays a fact
	// after the task is resolved and the deadline stops being in the future.
	SLABreached bool `json:"slaBreached" gorm:"not null;default:false" ui:"widget:checkbox" doc:"True once the deadline elapsed with the task unresolved" default:"false" required:"false"`

	// ResolvedAt and Resolution close the loop. Both are set by Service.Resolve.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty" gorm:"type:timestamptz" ui:"widget:datetime;hide:list" doc:"When the task was resolved" readOnly:"true"`
	Resolution string     `json:"resolution,omitempty" gorm:"type:text" ui:"widget:textarea;hide:list" doc:"How the task was resolved"`
	// Revision counts this row's writes, from 1. It is what a diff can be quoted
	// against: a proposal records the number it was made at, and the apply refuses
	// unless the row is still on it (modules/change's stale-base rule), so a change
	// approved against one deadline can never land over another.
	//
	// The same reason modules/site's Settings gave for growing the column: "somebody
	// diffing these needs a number". Every Go write of the row moves it — the
	// generated PATCH, each of the three commands, and Writer.Save — which is why a
	// reviewer's stale-base refusal is always explainable. A database trigger was
	// refused: it would move the number on writes nobody diffs, invisible to the
	// column list crud.Update is handed, and the refusal it caused would name a
	// change nobody could point at.
	Revision int64 `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`

	// Changes is what the save that published this payload moved. It is the trail's
	// field and not the entity's: see events.Change and events.Recorder. The kernel's
	// own write door fills it from the row that write made, just before it publishes
	// task.task.updated, and clears it before the response is written. The create and the
	// delete share this payload type and never carry it: only a save replaces a row, and
	// the create door discards a changes array a body arrived with rather than publish a
	// history nobody lived. It is
	// no column, no form field, no filter and no REST property — a slice of structs is
	// no entity field type, the gorm tag drops it from the table and the huma tag drops
	// it from the REST document — so the one event that ever writes it is the update,
	// which is where "what changed" has to be to still be readable in a year.
	Changes []events.Change `json:"changes,omitempty" gorm:"-" hidden:"true"`
}

// SetChanges is events.Recorder: it lets the kernel's CRUD door report what this
// save replaced, in the event that says the save happened.
func (t *Task) SetChanges(changes []events.Change) {
	t.Changes = changes
}

// CommandOwnedFields are the fields a route of their own owns. rest.Spec's
// Immutable reads this list, so one sentence is the source for the PATCH refusal,
// the apply refusal and — through the composition — the gate's answer. A second
// spelling of the same four names would be one more thing to keep honest.
var CommandOwnedFields = []string{"assigneeId", "slaBreached", "resolvedAt", "resolution"}

// ProtectableFields names the fields of a task a proposal may move — which is the
// same list, from two sides, as "the fields an installation may put behind change
// control": a field no apply could write is a field no queue could review, and a
// field a reviewer must see is a field nobody should type directly. Two readers, one
// sentence, so neither can drift: the composition intersects it with the
// installation's own switch, and Writer.Save writes exactly these columns.
//
// A task is an SLA object, so the deadline and the urgency in the promise are the two
// worth a second pair of eyes; title and description are here because a queue that
// could not correct a mis-typed title is a queue people route around.
//
// What is not here is a fact of the same kind. The four CommandOwnedFields belong to
// their commands outright, and so does status: Assign moves open to acknowledged and
// publishes task.assigned, Resolve sets resolvedAt and publishes task.resolved, and a
// proposal that could set either field alone would leave a person responsible for a
// task nobody resolved. This list is the module's vocabulary and not a decision —
// which of these fields is protected at which installation is answered by the
// composition's Protection over the installation's own switch.
var ProtectableFields = []string{"title", "description", "priority", "dueAt", "slaDeadline", "source", "sourceRef"}

// TableName pins the table, so the entity and migrations/000004 agree.
func (Task) TableName() string { return "tasks" }

// IsOverdue reports whether the deadline has passed with the task unresolved.
// It is the one definition of a breach: the sweep in internal/sla.go and
// Service.CheckSLA both ask it rather than comparing timestamps themselves.
func (t *Task) IsOverdue(now time.Time) bool {
	if t.SLADeadline == nil || t.ResolvedAt != nil {
		return false
	}
	return now.After(*t.SLADeadline)
}

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses: a title that differs
// only in whitespace is the same title, and two callers must not disagree.
func (t *Task) Validate(context.Context) error {
	// Match the JSON schema and varchar limit in characters, before normalization.
	if utf8.RuneCountInString(t.Title) > 200 {
		return fmt.Errorf("title must have at most 200 characters")
	}
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return fmt.Errorf("a task needs a title")
	}
	if t.Status == "" {
		t.Status = StatusOpen
	}
	if !slices.Contains(statuses, t.Status) {
		return fmt.Errorf("status %q is not a lifecycle state", t.Status)
	}
	if t.Priority == "" {
		t.Priority = PriorityNormal
	}
	if !slices.Contains(priorities, t.Priority) {
		return fmt.Errorf("priority %q is not a priority", t.Priority)
	}
	if t.SLABreached && t.SLADeadline == nil {
		return fmt.Errorf("a breach needs the deadline it broke")
	}
	// Both halves of "resolved means resolved at a time", so a resolved task
	// can always answer when.
	resolved := t.Status == StatusResolved || t.Status == StatusClosed
	if t.ResolvedAt != nil && !resolved {
		return fmt.Errorf("a %s task cannot have a resolution time", t.Status)
	}
	if resolved && t.ResolvedAt == nil {
		return fmt.Errorf("a %s task needs a resolution time", t.Status)
	}
	return nil
}

// Service is the task lifecycle: the three transitions generic CRUD cannot
// safely infer, because each is a rule about the state it came from and each
// publishes an event. Everything else about a task is the five routes kit/rest
// mounts.
//
// Every command takes the caller's transaction rather than opening one. An HTTP
// handler, a periodic job and another module's event handler each already hold
// one, and the state change and its event belong in that one — which is also
// why the implementation needs no dependencies at all.
//
// The three errors a caller can act on are kit/crud's, so one mapping answers
// for the lifecycle routes and the CRUD routes alike:
//
//	crud.ErrNotFound  no such task in this tenant
//	crud.ErrConflict  the task is in a state that refuses this command
//	crud.ErrInvalid   the argument is not one the command can use
//
// Each command is idempotent when repeated with the same argument: the callers
// that retry — a browser, a redelivered event, the next sweep — must not each
// produce an event. Concurrent commands serialize their decisions on the task
// until the caller commits or rolls back. At read committed, a waiter evaluates
// the committed state; a stricter isolation level may return a serialization
// failure, which requires retrying the caller's whole transaction.
type Service interface {
	// Assign makes assignee responsible and acknowledges an open task. The
	// same person again changes nothing and publishes nothing.
	Assign(ctx context.Context, tx db.Tx[db.Tenant], id, assignee uuid.UUID) (*Task, error)

	// Resolve closes the loop. The same resolution again, or none, changes
	// nothing; a different one is a conflict, because a resolved task's account
	// of itself is not something a retry may quietly rewrite.
	Resolve(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, resolution string) (*Task, error)

	// CheckSLA records a breach if the deadline has passed and the task is
	// neither resolved nor already breached. The sweep calls it once a minute
	// for every overdue task, so it publishes at most once per task.
	CheckSLA(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Task, error)
}

// LockedReader is the read a diff is made against: the task with its row locked
// FOR UPDATE, so the revision beside the diff is the revision nobody else can move
// while this transaction is open.
//
// It is a second interface and not a method on Service for the reason modules/site
// gives for its own: adding a method to an exported interface is an API break for
// every consumer that implemented it, and a caller that needs the plain lifecycle
// should not be handed the locked row by the interface it already had.
type LockedReader interface {
	TaskForUpdate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Task, error)
}

// Writer is the door an approved proposal comes through: the whole-row write, on
// the fields a proposal is allowed to carry.
//
// It is not a method on Service, for the same reason LockedReader is not, and it is
// not the generic PATCH: a PATCH refuses an immutable field with a 422, and a
// merged proposal document is not a caller reaching for a command-owned field, it
// is the *current* row with one field changed. So this method checks the merged
// document against the locked row rather than against a request body, and refuses
// (crud.ErrInvalid, naming the field) one that moves a command-owned field or the
// status — because assigneeId belongs to Assign, which moves the status and
// publishes task.assigned, and a proposal that wrote the field alone would leave a
// person responsible and nobody told.
//
// The write publishes task.task.updated like any other write of the row and moves
// revision, so the apply leaves a trail and a number. There is exactly one such
// door, it is not behind the gate (a gate on both sides is a wall), and what makes
// it safe is that it is named, single and audited — not that it is hidden.
type Writer interface {
	Save(ctx context.Context, tx db.Tx[db.Tenant], in *Task) (*Task, error)
}
