// Package tasktest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification of the lifecycle written as executable cases; the real service
// and the fake both run it, so "the fake behaves like the real thing" is a test
// result rather than a hope, and a consumer that tests against the fake is
// testing against the rules the database enforces.
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
	// lifecycle and creating a task is kit/crud's five routes.
	Seed func(*contracts.Task) uuid.UUID
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

// silent runs step and fails if it published anything. It is how the suite
// says "and this changed nothing", which is the claim a retry depends on.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
}

// Harness builds one Fixture and calls run with it. It is written this way
// round — the harness calling the case rather than returning to it — because
// the real service's fixture is a transaction, and a transaction is a scope
// somebody has to close: the harness wraps the case in db.Run and rolls back on
// the way out, which a Harness that only returned a Fixture could not do.
//
// RunService calls it once per case, so no case sees another's rows.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// past and future are deadlines either side of now, far enough out that a slow
// test machine cannot cross one while the case runs.
func past() *time.Time   { at := time.Now().Add(-time.Hour); return &at }
func future() *time.Time { at := time.Now().Add(time.Hour); return &at }

// open is a task in the state everything starts in.
func open(title string) *contracts.Task {
	return &contracts.Task{Title: title, Status: contracts.StatusOpen, Priority: contracts.PriorityHigh}
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"assign acknowledges an open task": func(t *testing.T, f Fixture) {
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

		"assign requires an assignee": func(t *testing.T, f Fixture) {
			_, err := f.Service.Assign(f.Ctx, f.Tx, f.Seed(open("chiller")), uuid.Nil)
			mustBe(t, err, crud.ErrInvalid)
		},

		"assign is idempotent for the same assignee": func(t *testing.T, f Fixture) {
			id, who := f.Seed(open("chiller")), uuid.New()
			before := len(f.Published())
			if _, err := f.Service.Assign(f.Ctx, f.Tx, id, who); err != nil {
				t.Fatalf("the first Assign: %v", err)
			}
			if len(f.Published()) != before+1 {
				t.Fatalf("the first Assign published %v, want one event", f.Published()[before:])
			}
			var got *contracts.Task
			f.silent(t, "assigning the same person again", func() {
				var err error
				if got, err = f.Service.Assign(f.Ctx, f.Tx, id, who); err != nil {
					t.Fatalf("the second Assign: %v", err)
				}
			})
			if got.Status != contracts.StatusAcknowledged || *got.AssigneeID != who {
				t.Errorf("the second Assign left %q/%v", got.Status, got.AssigneeID)
			}
		},

		"resolve records the resolution and the time": func(t *testing.T, f Fixture) {
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

		"resolve is idempotent for the same resolution": func(t *testing.T, f Fixture) {
			id := f.Seed(open("chiller"))
			before := len(f.Published())
			first, err := f.Service.Resolve(f.Ctx, f.Tx, id, "swapped the valve")
			if err != nil {
				t.Fatalf("the first Resolve: %v", err)
			}
			if len(f.Published()) != before+1 {
				t.Fatalf("the first Resolve published %v, want one event", f.Published()[before:])
			}
			for _, again := range []string{"swapped the valve", ""} {
				f.silent(t, fmt.Sprintf("Resolve(%q) after resolving", again), func() {
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

		"resolve refuses a different resolution": func(t *testing.T, f Fixture) {
			id := f.Seed(open("chiller"))
			if _, err := f.Service.Resolve(f.Ctx, f.Tx, id, "swapped the valve"); err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			_, err := f.Service.Resolve(f.Ctx, f.Tx, id, "it fixed itself")
			mustBe(t, err, crud.ErrConflict)
		},

		"check-sla flags an overdue task once": func(t *testing.T, f Fixture) {
			overdue := open("chiller")
			overdue.SLADeadline = past()
			id := f.Seed(overdue)

			before := len(f.Published())
			got, err := f.Service.CheckSLA(f.Ctx, f.Tx, id)
			if err != nil {
				t.Fatalf("CheckSLA: %v", err)
			}
			if !got.SLABreached {
				t.Fatal("a deadline an hour ago with the task unresolved is a breach")
			}
			if len(f.Published()) != before+1 {
				t.Fatalf("the first CheckSLA published %v, want one event", f.Published()[before:])
			}
			// The sweep runs every minute forever, so the second call is the
			// ordinary case: it changes nothing and, above all, says nothing.
			f.silent(t, "the second CheckSLA", func() {
				if got, err = f.Service.CheckSLA(f.Ctx, f.Tx, id); err != nil || !got.SLABreached {
					t.Errorf("the second CheckSLA = %v, %v", got, err)
				}
			})
		},

		"check-sla leaves a future deadline alone": func(t *testing.T, f Fixture) {
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

		"a resolved task cannot be assigned": func(t *testing.T, f Fixture) {
			for _, status := range []string{contracts.StatusResolved, contracts.StatusClosed} {
				done := open("chiller")
				at := time.Now()
				done.Status, done.ResolvedAt = status, &at
				_, err := f.Service.Assign(f.Ctx, f.Tx, f.Seed(done), uuid.New())
				mustBe(t, err, crud.ErrConflict)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			if _, err := f.Service.Assign(f.Ctx, f.Tx, id, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Assign of an unknown task = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Resolve(f.Ctx, f.Tx, id, "x"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Resolve of an unknown task = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.CheckSLA(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("CheckSLA of an unknown task = %v, want ErrNotFound", err)
			}
		},
	}
}

func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
