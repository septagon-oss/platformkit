// Package tasktest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification of the lifecycle written as executable cases; the real service
// and the fake both run it, so "the fake behaves like the real thing" is a test
// result rather than a hope, and a consumer that tests against the fake is
// testing against the rules the database enforces.
//
// The suite is a kit/porttest description: the operations, what each publishes,
// and which calls it must refuse. What that buys over the ten hand-written cases
// it replaces is eight more cases — an unknown row at every command instead of
// one case covering three, and the retry at every command — and nine written
// reasons where a case is not owed. It costs lines rather than saving them at
// this size, which the description's own note says: the win scales with refusals
// per operation, and this port has four.
package tasktest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a store to put tasks in. The transaction is the real thing for the real
// service and the zero value for the fake, which never looks at it.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Seed stores a task and returns the id it was given. It is the one thing
	// the suite cannot do through the interface, because the interface is the
	// lifecycle and creating a task is kit/rest's five routes.
	Seed func(*contracts.Task) uuid.UUID
	// Task reads one back. The suite needs it to say what "a refused command
	// wrote nothing" means: the snapshot either side of a refusal is this task,
	// rendered.
	Task func(uuid.UUID) (contracts.Task, error)
	// Published is the events the implementation has published so far, in
	// order. The fake returns what it recorded; the real service's harness
	// reads the outbox rows its transaction has written.
	//
	// It is part of the fixture because half of what the lifecycle promises is
	// silence: every command is idempotent, and an idempotent command that
	// publishes is a subscriber told twice about one thing. A suite that could
	// only see return values could not check that, and it is the half a retry
	// exercises every day.
	Published func() []string
}

// Harness builds one Fixture and calls run with it. It is written this way
// round — the harness calling the case rather than returning to it — because
// the real service's fixture is a transaction, and a transaction is a scope
// somebody has to close: the harness wraps the case in db.Run and rolls back on
// the way out, which a Harness that only returned a Fixture could not do.
//
// The suite calls it once per case, so no case sees another's rows.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	porttest.Run(t, Suite(h))
}

// Suite is the port, described. A consumer that wants the case names without
// running them reads porttest.Names(tasktest.Suite(h)).
func Suite(h Harness) porttest.Suite[Fixture] {
	return porttest.Suite[Fixture]{
		Port:     "contracts.Service",
		World:    h,
		Events:   func(f Fixture) []string { return f.Published() },
		Classify: classify,
		Ops: []porttest.Op[Fixture]{
			{
				Name: "Assign", Mutates: true,
				Publishes: []string{contracts.EventAssigned},
				Ready:     func(_ *testing.T, f Fixture) uuid.UUID { return f.Seed(open("chiller")) },
				Call:      func(f Fixture, row uuid.UUID) (string, error) { return assign(f, row, assignee) },
				Snapshot:  snapshot,
				Names:     map[porttest.Kind]string{porttest.Retry: "assign is idempotent for the same assignee"},
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return assign(f, uuid.New(), assignee) }),
						Is:   is(crud.ErrNotFound)},
					{Name: "assign requires an assignee", Class: porttest.Correctable,
						Call: refuse(func(f Fixture, row uuid.UUID) (string, error) { return assign(f, row, uuid.Nil) }),
						Is:   is(crud.ErrInvalid)},
					// Two entries and not one with a loop in it: the case this
					// replaces walked [resolved, closed] and a refusal has one
					// row. The split names the two states.
					{Name: "a resolved task cannot be assigned", Class: porttest.Immutable,
						Provoke: func(_ *testing.T, f Fixture, _ uuid.UUID) uuid.UUID { return f.Seed(done(contracts.StatusResolved)) },
						Call:    refuse(func(f Fixture, row uuid.UUID) (string, error) { return assign(f, row, assignee) }),
						Is:      is(crud.ErrConflict)},
					{Name: "a closed task cannot be assigned", Class: porttest.Immutable,
						Provoke: func(_ *testing.T, f Fixture, _ uuid.UUID) uuid.UUID { return f.Seed(done(contracts.StatusClosed)) },
						Call:    refuse(func(f Fixture, row uuid.UUID) (string, error) { return assign(f, row, assignee) }),
						Is:      is(crud.ErrConflict)},
				},
				Skip: notOwedHere,
			},
			{
				Name: "Resolve", Mutates: true,
				Publishes: []string{contracts.EventResolved},
				Ready:     func(_ *testing.T, f Fixture) uuid.UUID { return f.Seed(open("chiller")) },
				Call:      func(f Fixture, row uuid.UUID) (string, error) { return resolve(f, row, "swapped the valve") },
				Snapshot:  snapshot,
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return resolve(f, uuid.New(), "x") }),
						Is:   is(crud.ErrNotFound)},
					{Name: "resolve refuses a different resolution", Class: porttest.Immutable,
						Provoke: func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
							if _, err := resolve(f, row, "swapped the valve"); err != nil {
								t.Fatalf("Resolve: %v", err)
							}
							return row
						},
						Call: refuse(func(f Fixture, row uuid.UUID) (string, error) { return resolve(f, row, "it fixed itself") }),
						Is:   is(crud.ErrConflict)},
				},
				Skip: notOwedHere,
			},
			{
				Name: "CheckSLA", Mutates: true,
				Publishes: []string{contracts.EventSLABreached},
				Ready:     func(_ *testing.T, f Fixture) uuid.UUID { return f.Seed(overdue()) },
				Call:      checkSLA,
				Snapshot:  snapshot,
				// No name of the module's own for the success or the retry here:
				// "check-sla flags an overdue task once" asserts the breach flag
				// as well as the event, so a generated case that took its name
				// would take its name and not its assertion. It stays below, and
				// the generated cases run beside it.
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return checkSLA(f, uuid.New()) }),
						Is:   is(crud.ErrNotFound)},
				},
				Skip: notOwedHere,
			},
		},
		Own: cases(),
	}
}

// notOwedHere is the three floor cases this port does not owe, with the reason
// each is not asked. They are the same for all three commands.
var notOwedHere = map[porttest.Kind]string{
	porttest.Denied: "the lifecycle takes no grant of its own: the three commands are reached " +
		"through kit/rest's routes, and that is where the grant is checked",
	porttest.Stale: "a task carries no revision; kit/crud's own PATCH owns that check",
	porttest.Elsewhere: "the world is one tenant's transaction, so there is no second tenant " +
		"here to make the refused call from. The fake partitions its store by the tenant on the " +
		"context and the real service runs under row-level security, which kit/db's " +
		"TestTenantIsolationIsEnforcedByPostgres and kit/crud's TestAnotherTenantReachesNothing " +
		"prove against the schema",
}

// assignee is the person every case assigns to, so that "the same assignee"
// means the same person in the retry.
var assignee = uuid.New()

func is(want error) func(Fixture, error) bool {
	return func(_ Fixture, err error) bool { return errors.Is(err, want) }
}

// classify is this port's reading of its own refusals: an input the caller can
// retype, or a state that forbids the call however it is retyped.
func classify(err error) porttest.Class {
	switch {
	case errors.Is(err, crud.ErrInvalid):
		return porttest.Correctable
	case errors.Is(err, crud.ErrConflict), errors.Is(err, crud.ErrNotFound):
		return porttest.Immutable
	default:
		return porttest.Unclassified
	}
}

// assertions is the floor a generated case gets, for a case that is written by
// hand: the same reading of silence, refusal and "wrote nothing". It is built
// from the description this case belongs to rather than restated beside it, so
// a port that changes how it classifies a refusal changes it in one place. The
// harness it is taken from needs no world of its own here: the case already has
// the one it was handed.
func assertions(t *testing.T, f Fixture) porttest.World[Fixture] {
	return Suite(nil).Assert(t, f)
}

func assign(f Fixture, row, who uuid.UUID) (string, error) {
	got, err := f.Service.Assign(f.Ctx, f.Tx, row, who)
	return answer(got), err
}

func resolve(f Fixture, row uuid.UUID, text string) (string, error) {
	got, err := f.Service.Resolve(f.Ctx, f.Tx, row, text)
	return answer(got), err
}

func checkSLA(f Fixture, row uuid.UUID) (string, error) {
	got, err := f.Service.CheckSLA(f.Ctx, f.Tx, row)
	return answer(got), err
}

// refuse is an operation's call as a refusal reads it: the error alone, because
// what a refused call left behind is the harness's assertion and not the port's.
func refuse(call func(Fixture, uuid.UUID) (string, error)) func(Fixture, uuid.UUID) error {
	return func(f Fixture, row uuid.UUID) error {
		_, err := call(f, row)
		return err
	}
}

// answer is what a command handed back, rendered so that two answers compare
// with ==. The retry reads it either side of the second call, which is where
// "assign is idempotent for the same assignee" says its second half: a command
// that stores the right task and answers another has handed its caller a task
// nothing wrote, and no snapshot of the store can see that.
func answer(task *contracts.Task) string {
	if task == nil {
		return "nothing"
	}
	return fmt.Sprintf("status=%s assignee=%s resolution=%q resolved=%s breached=%v",
		task.Status, id(task.AssigneeID), task.Resolution, at(task.ResolvedAt), task.SLABreached)
}

// snapshot is everything the three commands can write, rendered so that two
// readings compare with ==. The resolution time is in it because a retry that
// moved it would be a loop closing twice.
func snapshot(t *testing.T, f Fixture, row uuid.UUID) string {
	t.Helper()
	task, err := f.Task(row)
	if err != nil {
		return "no such task"
	}
	return answer(&task)
}

func id(who *uuid.UUID) string {
	if who == nil {
		return "nobody"
	}
	return who.String()
}

func at(when *time.Time) string {
	if when == nil {
		return "never"
	}
	return when.UTC().Format(time.RFC3339Nano)
}

// past and future are deadlines either side of now, far enough out that a slow
// test machine cannot cross one while the case runs.
func past() *time.Time   { at := time.Now().Add(-time.Hour); return &at }
func future() *time.Time { at := time.Now().Add(time.Hour); return &at }

// open is a task in the state everything starts in.
func open(title string) *contracts.Task {
	return &contracts.Task{Title: title, Status: contracts.StatusOpen, Priority: contracts.PriorityHigh}
}

// done is a task the loop has already closed.
func done(status string) *contracts.Task {
	at := time.Now()
	task := open("chiller")
	task.Status, task.ResolvedAt = status, &at
	return task
}

// overdue is a task whose deadline went by an hour ago.
func overdue() *contracts.Task {
	task := open("chiller")
	task.SLADeadline = past()
	return task
}

// cases is what the description cannot express, each with the reason it is
// written by hand.
func cases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "assign acknowledges an open task",
			Because: "what a success leaves behind is the port's domain: taking a task is acknowledging it, and the assignee is the person named",
			Run: func(t *testing.T, f Fixture) {
				id, who := f.Seed(open("chiller")), uuid.New()
				got, err := f.Service.Assign(f.Ctx, f.Tx, id, who)
				if err != nil {
					t.Fatalf("Assign: %v", err)
				}
				if got.Status != contracts.StatusAcknowledged {
					t.Errorf("status is %q, want %q: taking a task is acknowledging it",
						got.Status, contracts.StatusAcknowledged)
				}
				if got.AssigneeID == nil || *got.AssigneeID != who {
					t.Errorf("assignee is %v, want %s", got.AssigneeID, who)
				}
			},
		},
		{
			Name:    "resolve records the resolution and the time",
			Because: "domain contents: the text is trimmed so two callers cannot disagree about whitespace, and the resolution time is stamped",
			Run: func(t *testing.T, f Fixture) {
				got, err := f.Service.Resolve(f.Ctx, f.Tx, f.Seed(open("chiller")), "  swapped the valve  ")
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if got.Status != contracts.StatusResolved {
					t.Errorf("status is %q, want %q", got.Status, contracts.StatusResolved)
				}
				if got.Resolution != "swapped the valve" {
					t.Errorf("resolution is %q; it is trimmed, so two callers cannot disagree about whitespace", got.Resolution)
				}
				if got.ResolvedAt == nil {
					t.Error("a resolved task has no resolution time")
				}
			},
		},
		{
			Name:    "resolve is idempotent for the same resolution",
			Because: "the retry is asked twice, with the same text and with none at all, and neither may move the resolution time; a generated retry makes one call",
			Run: func(t *testing.T, f Fixture) {
				world := assertions(t, f)
				id := f.Seed(open("chiller"))
				first, err := f.Service.Resolve(f.Ctx, f.Tx, id, "swapped the valve")
				if err != nil {
					t.Fatalf("the first Resolve: %v", err)
				}
				for _, again := range []string{"swapped the valve", ""} {
					world.Silent(fmt.Sprintf("Resolve(%q) after resolving", again), func() {
						got, err := f.Service.Resolve(f.Ctx, f.Tx, id, again)
						if err != nil {
							t.Fatalf("Resolve(%q) after resolving: %v", again, err)
						}
						if !got.ResolvedAt.Equal(*first.ResolvedAt) {
							t.Errorf("Resolve(%q) moved the resolution time; the loop closed once", again)
						}
					})
				}
			},
		},
		{
			Name:    "check-sla flags an overdue task once",
			Because: "domain contents: the breach flag itself, and the sweep's second call, which runs every minute forever and must change nothing",
			Run: func(t *testing.T, f Fixture) {
				world := assertions(t, f)
				id := f.Seed(overdue())
				got, err := f.Service.CheckSLA(f.Ctx, f.Tx, id)
				if err != nil {
					t.Fatalf("CheckSLA: %v", err)
				}
				if !got.SLABreached {
					t.Fatal("a deadline an hour ago with the task unresolved is a breach")
				}
				world.Silent("the second CheckSLA", func() {
					if got, err = f.Service.CheckSLA(f.Ctx, f.Tx, id); err != nil || !got.SLABreached {
						t.Errorf("the second CheckSLA = %v, %v", got, err)
					}
				})
			},
		},
		{
			Name:    "check-sla leaves a future deadline alone",
			Because: "domain: a deadline an hour from now is not a breach yet, which is a success that publishes nothing",
			Run: func(t *testing.T, f Fixture) {
				soon := open("chiller")
				soon.SLADeadline = future()
				got, err := f.Service.CheckSLA(f.Ctx, f.Tx, f.Seed(soon))
				if err != nil {
					t.Fatalf("CheckSLA: %v", err)
				}
				if got.SLABreached {
					t.Error("a deadline an hour from now is not a breach yet")
				}
			},
		},
	}
}
