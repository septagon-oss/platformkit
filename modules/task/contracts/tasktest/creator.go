package tasktest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"slices"
)

// RunCreator is the conformance suite for contracts.Creator and contracts.Reader.
// It is a suite of its own and not an operation of Suite: a create has no row to
// retry on, no unknown row and no revision to be stale about, so the floor
// porttest generates for a command on a row does not describe it. What it owes
// instead is written below as cases, each with the reason it is hand-written.
func RunCreator(t *testing.T, h Harness) {
	t.Helper()
	porttest.Run(t, CreatorSuite(h))
}

// CreatorSuite is RunCreator's description, for a consumer that wants the names.
func CreatorSuite(h Harness) porttest.Suite[Fixture] {
	s := Suite(h)
	return porttest.Suite[Fixture]{
		Port: "contracts.Creator", World: h, Events: s.Events, Classify: s.Classify,
		Own: creatorCases(),
	}
}

// created calls Create and fails the case on an error.
func created(t *testing.T, f Fixture, in contracts.CreateTask) *contracts.Task {
	t.Helper()
	got, err := f.Creator.Create(f.Ctx, f.Tx, in)
	if err != nil {
		t.Fatalf("Create(%+v): %v", in, err)
	}
	return got
}

func publishedAfter(f Fixture, before int) []string { return slices.Clone(f.Published()[before:]) }

func creatorCases() []porttest.Case[Fixture] {
	var all []porttest.Case[Fixture]
	for _, group := range [][]porttest.Case[Fixture]{storedCases(), breachCases(), refusalCases(), rollbackCases()} {
		all = append(all, group...)
	}
	return all
}

// storedCases are what a successful create leaves: the row, its events and its read.
func storedCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "create stores the defaults and a server identity",
			Because: "domain contents: what the caller left unsaid, what the server chose, and that the answer is the stored row",
			Run: func(t *testing.T, f Fixture) {
				got := created(t, f, contracts.CreateTask{Title: "  chiller  "})
				if got.ID == uuid.Nil || got.Revision != 1 || got.SLABreached || got.ResolvedAt != nil {
					t.Errorf("identity is %s revision %d breached %v resolved %v", got.ID, got.Revision, got.SLABreached, got.ResolvedAt)
				}
				if got.Title != "chiller" || got.Status != contracts.StatusOpen || got.Priority != contracts.PriorityNormal {
					t.Errorf("defaults are %q %q %q, want chiller open normal", got.Title, got.Status, got.Priority)
				}
				stored, err := f.Task(got.ID)
				if err != nil || answer(&stored) != answer(got) || stored.TenantID == uuid.Nil {
					t.Errorf("the stored task is %v, %v; the answer was %s", stored, err, answer(got))
				}
			},
		},
		{
			Name:    "create publishes the created event once",
			Because: "the events are the half of the outcome a return value cannot show; one create, one event, no breach and no assignment events beside it",
			Run: func(t *testing.T, f Fixture) {
				before := len(f.Published())
				created(t, f, contracts.CreateTask{Title: "chiller"})
				if got := publishedAfter(f, before); !slices.Equal(got, []string{contracts.EventCreated}) {
					t.Errorf("published %v, want exactly %s", got, contracts.EventCreated)
				}
			},
		},
		{
			Name:    "two creates are two tasks",
			Because: "a create is not idempotent: the key that makes a retry safe belongs to the caller, and this pins that the command does not invent one",
			Run: func(t *testing.T, f Fixture) {
				before, events := f.Tasks(), len(f.Published())
				a, b := created(t, f, contracts.CreateTask{Title: "chiller"}), created(t, f, contracts.CreateTask{Title: "chiller"})
				if a.ID == b.ID || f.Tasks() != before+2 || len(f.Published()) != events+2 {
					t.Errorf("two identical creates left %d tasks and %d events more", f.Tasks()-before, len(f.Published())-events)
				}
			},
		},
	}
}

// breachCases are the SLA breach on arrival and the assignment a create may carry.
func breachCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "a past slaDeadline records a breach and its event once",
			Because: "breach on arrival: the created event describes the row as inserted and the breach follows it, in the same transaction",
			Run: func(t *testing.T, f Fixture) {
				before := len(f.Published())
				got := created(t, f, contracts.CreateTask{Title: "chiller", SLADeadline: past()})
				want := []string{contracts.EventCreated, contracts.EventSLABreached}
				if events := publishedAfter(f, before); !slices.Equal(events, want) {
					t.Errorf("published %v, want %v", events, want)
				}
				if stored, _ := f.Task(got.ID); !got.SLABreached || !stored.SLABreached {
					t.Errorf("breached: answer %v, stored %v", got.SLABreached, stored.SLABreached)
				}
				assertions(t, f).Silent("the sweep's CheckSLA after the breach on arrival", func() {
					if _, err := f.Service.CheckSLA(f.Ctx, f.Tx, got.ID); err != nil {
						t.Errorf("CheckSLA: %v", err)
					}
				})
			},
		},
		{
			Name:    "a past dueAt with no slaDeadline records no breach",
			Because: "domain: dueAt is the soft target, and only slaDeadline is a promise that can be broken",
			Run: func(t *testing.T, f Fixture) {
				before := len(f.Published())
				got := created(t, f, contracts.CreateTask{Title: "chiller", DueAt: past()})
				if events := publishedAfter(f, before); got.SLABreached || !slices.Equal(events, []string{contracts.EventCreated}) {
					t.Errorf("breached %v, published %v; a late soft target is no breach", got.SLABreached, events)
				}
			},
		},
		{
			Name:    "a future slaDeadline records no breach",
			Because: "domain: the deadline is stored and the promise is not yet broken",
			Run: func(t *testing.T, f Fixture) {
				before := len(f.Published())
				got := created(t, f, contracts.CreateTask{Title: "chiller", SLADeadline: future()})
				if got.SLABreached || got.SLADeadline == nil || len(publishedAfter(f, before)) != 1 {
					t.Errorf("breached %v deadline %v events %v", got.SLABreached, got.SLADeadline, publishedAfter(f, before))
				}
			},
		},
		{
			Name:    "an assignee acknowledges the task and publishes the assignment",
			Because: "domain: the person named is responsible, so the workload subscriber is told, exactly as Assign tells it",
			Run: func(t *testing.T, f Fixture) {
				who, before := uuid.New(), len(f.Published())
				got := created(t, f, contracts.CreateTask{Title: "chiller", AssigneeID: &who})
				want := []string{contracts.EventCreated, contracts.EventAssigned}
				if events := publishedAfter(f, before); !slices.Equal(events, want) {
					t.Errorf("published %v, want %v", events, want)
				}
				if got.Status != contracts.StatusAcknowledged || got.AssigneeID == nil || *got.AssigneeID != who {
					t.Errorf("status %q assignee %v, want acknowledged %s", got.Status, got.AssigneeID, who)
				}
			},
		},
	}
}

// refusalCases are the correctable refusals of the input and the read of what was made.
func refusalCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "a refused create writes nothing and says nothing",
			Because: "every correctable refusal of the input, each with no row to snapshot: the count of tasks and the events are the comparison",
			Run: func(t *testing.T, f Fixture) {
				nobody, zero := uuid.Nil, time.Time{}
				for _, in := range map[string]contracts.CreateTask{
					"an empty title":        {Title: ""},
					"a blank title":         {Title: " \t "},
					"a title of 201 chars":  {Title: strings.Repeat("é", 201)},
					"an unknown status":     {Title: "x", Status: "paused"},
					"an unknown priority":   {Title: "x", Priority: "urgent"},
					"a resolved task":       {Title: "x", Status: contracts.StatusResolved},
					"a closed task":         {Title: "x", Status: contracts.StatusClosed},
					"a source of 41 chars":  {Title: "x", Source: strings.Repeat("s", 41)},
					"a sourceRef of 121":    {Title: "x", SourceRef: strings.Repeat("r", 121)},
					"an assignee of nobody": {Title: "x", AssigneeID: &nobody},
					"a zero deadline":       {Title: "x", SLADeadline: &zero},
				} {
					assertions(t, f).Refused(func(err error) bool { return errors.Is(err, crud.ErrInvalid) }, porttest.Correctable,
						func() string { return fmt.Sprint(f.Tasks()) },
						func() error { _, err := f.Creator.Create(f.Ctx, f.Tx, in); return err })
				}
			},
		},
		{
			Name:    "the reader returns the created task and refuses an unknown one",
			Because: "contracts.Reader is the plain read of what Creator made; an unknown id is crud.ErrNotFound, immutable",
			Run: func(t *testing.T, f Fixture) {
				got := created(t, f, contracts.CreateTask{Title: "chiller"})
				read, err := f.Reader.TaskByID(f.Ctx, f.Tx, got.ID)
				if err != nil || answer(read) != answer(got) {
					t.Errorf("TaskByID = %v, %v; want the created task", read, err)
				}
				if _, err := f.Reader.TaskByID(f.Ctx, f.Tx, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("an unknown id answered %v, want crud.ErrNotFound", err)
				}
			},
		},
	}
}

// rollbackCases need a real transaction: a refusal or a failing outbox insert undoes the caller's write too.
func rollbackCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "a refused create takes the caller's write back with it",
			Because: "real-only: the fake ignores the transaction it is handed, so only a world with one can show that the caller's own write rolls back",
			Run: func(t *testing.T, f Fixture) {
				if f.Atomic == nil {
					t.Skip("this world has no transaction to roll back")
				}
				a := f.Atomic
				before, rows := f.Tasks(), a.Rows()
				err := a.Attempt(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					if err := a.Wrote(ctx, tx); err != nil {
						return err
					}
					_, err := f.Creator.Create(ctx, tx, contracts.CreateTask{Title: ""})
					return err
				})
				if !errors.Is(err, crud.ErrInvalid) || f.Tasks() != before || a.Rows() != rows {
					t.Errorf("err %v, tasks %d (was %d), caller rows %d (was %d)", err, f.Tasks(), before, a.Rows(), rows)
				}
			},
		},
		{
			Name:    "a failing outbox insert takes the created task back with it",
			Because: "real-only: the outbox row is written in the caller's transaction, so its failure must undo the row, the caller's write and the events already written; injected at the insert, because the transport runs after commit",
			Run: func(t *testing.T, f Fixture) {
				if f.Atomic == nil {
					t.Skip("this world has no transaction to roll back")
				}
				a := f.Atomic
				for _, failing := range []string{contracts.EventCreated, contracts.EventSLABreached} {
					undo := a.FailOutbox(failing)
					before, rows, events := f.Tasks(), a.Rows(), len(f.Published())
					err := a.Attempt(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
						if err := a.Wrote(ctx, tx); err != nil {
							return err
						}
						_, err := f.Creator.Create(ctx, tx, contracts.CreateTask{Title: "chiller", SLADeadline: past()})
						return err
					})
					undo()
					if err == nil || f.Tasks() != before || a.Rows() != rows || len(f.Published()) != events {
						t.Errorf("failing %s: err %v, tasks %d (was %d), caller rows %d (was %d), events %d (was %d)",
							failing, err, f.Tasks(), before, a.Rows(), rows, len(f.Published()), events)
					}
				}
			},
		},
	}
}
