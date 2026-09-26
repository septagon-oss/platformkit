package porttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// This file is the port this package's own suite is about: the smallest port
// that can be got wrong in each of the ways a generated case exists to catch.
// One row, one grant, one revision, one event. The cases in porttest_test.go
// then run the generated suite against deliberately broken copies of the fake
// and assert that the case bites — a harness whose own assertions were never
// watched failing would be a harness nobody has tested.
//
// It is a fake of the shape modules/task/contracts/tasktest.Fake has, built on
// this package's own plumbing, because the kernel cannot import a module.

// note is the row: filed or not, at a revision, and sealed rows are refused.
type note struct {
	entity.Base
	Title  string
	Filed  bool
	Sealed bool
	Rev    int64
}

func (note) TableName() string { return "notes" }

// Validate is the check Seed runs, and the reason a fixture cannot seed a row
// the database would refuse.
func (n *note) Validate(context.Context) error {
	if n.Title == "" {
		return fmt.Errorf("%w: a note has a title", crud.ErrInvalid)
	}
	return nil
}

// The port's own refusals. They are the module's, not the kernel's: the harness
// asks the port whether an error is one of them.
var (
	errDenied = errors.New("notes: you may not file notes")
	errStale  = errors.New("notes: the note has moved")
	errSealed = errors.New("notes: a sealed note is filed once and for all")
)

const filePermission = "notes.file"

// knobs are the ways this fake can be wrong. Each one is the reason one case in
// porttest_test.go exists, and each is a bug a real fake has had.
type knobs struct {
	// writeFirst lands the write before the decision refuses it, which is the
	// bug a suite that only reads return values never sees.
	writeFirst bool
	// emitAlways says something on an idempotent retry, which is a subscriber
	// told twice about one thing.
	emitAlways bool
	// oneTenant keeps one set of rows for every tenant, which is what a fake
	// that never had a tenant key behaves like.
	oneTenant bool
	// answerMoves stores the right row and answers a revision it is not at,
	// which is a stale row handed to the caller: the bug a suite that reads the
	// store either side of a retry and never the answer cannot see.
	answerMoves bool
}

// notes is the fake service: this package's plumbing, one store, and the
// module's own decisions.
type notes struct {
	*Fake
	rows  *Store[note]
	knobs knobs
	// first is the tenant the broken copy pins its store to.
	first tenancy.Tenant
}

func newNotes(k knobs, first tenancy.Tenant) *notes {
	return &notes{
		Fake:  NewFake(time.Date(2026, 9, 26, 9, 0, 0, 0, time.UTC), []string{"note.filed"}),
		rows:  NewStore(func(n note) uuid.UUID { return n.ID }),
		knobs: k,
		first: first,
	}
}

// scope is the context the store is reached through: the caller's own, or — in
// the broken copy — the first tenant's, whatever tenant the caller is in. The
// actor is left alone, so the only thing that changes is the partition.
func (s *notes) scope(ctx context.Context) context.Context {
	if s.knobs.oneTenant {
		return tenancy.WithTenant(ctx, s.first)
	}
	return ctx
}

// File is the port's one command: grant, load, revision, decide, then write and
// say so. It answers the note as it now stands, which is what the caller of a
// command reads and what the retry compares either side of the second call.
func (s *notes) File(ctx context.Context, row uuid.UUID, expected int64) (note, error) {
	before, _ := s.rows.Get(s.scope(ctx), row)
	if s.knobs.writeFirst {
		if stored, err := s.rows.Get(s.scope(ctx), row); err == nil {
			stored.Filed, stored.Rev = true, stored.Rev+1
			s.rows.Put(s.scope(ctx), stored)
		}
	}
	filed, err := Do(s.scope(ctx), s.Fake, s.rows, Command[note]{
		Permission: filePermission,
		Denied:     errDenied,
		Row:        row,
		Expected:   expected,
		Revision:   func(n note) int64 { return n.Rev },
		Stale: func(stored, expected int64) error {
			return fmt.Errorf("%w: it is at %d and the caller read %d", errStale, stored, expected)
		},
		Decide: func(n note) error {
			if n.Sealed {
				return errSealed
			}
			return nil
		},
		Apply: func(n *note) []string {
			if n.Filed {
				if s.knobs.emitAlways {
					return []string{"note.filed"}
				}
				return nil
			}
			n.Filed, n.Rev = true, n.Rev+1
			return []string{"note.filed"}
		},
	})
	if err == nil && s.knobs.answerMoves && before.Filed {
		// Nothing was written — the note was filed already — and the caller is
		// told it moved all the same.
		filed.Rev++
	}
	return filed, err
}

// Note is the read a snapshot renders.
func (s *notes) Note(ctx context.Context, row uuid.UUID) (note, error) {
	return s.rows.Get(s.scope(ctx), row)
}

// world is one case's world, as a module's fixture is: the contexts the case
// acts through, the service and the seed door.
type world struct {
	ctx       context.Context // the tenant, and an actor who may file
	stranger  context.Context // the same tenant, an actor who holds nothing
	elsewhere context.Context // another tenant, the same actor
	service   *notes
	seed      func(t *testing.T, sealed bool) uuid.UUID
}

// noteWorld builds one world per case, with a service of its own so that no case
// sees another's rows.
func noteWorld(k knobs) func(*testing.T, func(world)) {
	return func(t *testing.T, run func(world)) {
		t.Helper()
		here := tenancy.Tenant{ID: uuid.New(), Slug: "here"}
		there := tenancy.Tenant{ID: uuid.New(), Slug: "there"}
		clerk, visitor := uuid.New(), uuid.New()
		service := newNotes(k, here)
		service.Grants.Grant(clerk, filePermission)

		ctx := tenancy.WithTenant(tenancy.WithActor(context.Background(), clerk), here)
		w := world{
			ctx:       ctx,
			stranger:  tenancy.WithTenant(tenancy.WithActor(context.Background(), visitor), here),
			elsewhere: tenancy.WithTenant(tenancy.WithActor(context.Background(), clerk), there),
			service:   service,
		}
		w.seed = func(t *testing.T, sealed bool) uuid.UUID {
			t.Helper()
			return Seed(ctx, service.Fake, service.rows, note{Title: "the minutes", Sealed: sealed})
		}
		run(w)
	}
}

// noteSuite is the port, described. It is what a module writes: closures beside
// the port, and a reason for every case the description cannot express.
func noteSuite(k knobs) Suite[world] {
	return Suite[world]{
		Port:   "notes.Service",
		World:  noteWorld(k),
		Events: func(w world) []string { return w.service.Events.Names() },
		Classify: func(err error) Class {
			switch {
			case errors.Is(err, errStale):
				return Correctable
			case errors.Is(err, errSealed):
				return Immutable
			default:
				return Unclassified
			}
		},
		Ops: []Op[world]{{
			Name:      "File",
			Mutates:   true,
			Publishes: []string{"note.filed"},
			Ready:     func(t *testing.T, w world) uuid.UUID { return w.seed(t, false) },
			Call: func(w world, row uuid.UUID) (string, error) {
				filed, err := w.service.File(w.ctx, row, revisionOf(w, row))
				return fmt.Sprintf("filed=%v rev=%d", filed.Filed, filed.Rev), err
			},
			Snapshot: func(t *testing.T, w world, row uuid.UUID) string {
				t.Helper()
				stored, err := w.service.Note(w.ctx, row)
				if err != nil {
					return "no such note"
				}
				return fmt.Sprintf("filed=%v sealed=%v rev=%d", stored.Filed, stored.Sealed, stored.Rev)
			},
			Refusals: []Refusal[world]{
				{Kind: Unknown,
					Call: func(w world, _ uuid.UUID) error {
						_, err := w.service.File(w.ctx, uuid.New(), 0)
						return err
					},
					Is: func(_ world, err error) bool { return errors.Is(err, crud.ErrNotFound) }},
				{Kind: Denied,
					Call: func(w world, row uuid.UUID) error {
						_, err := w.service.File(w.stranger, row, revisionOf(w, row))
						return err
					},
					Is: func(_ world, err error) bool { return errors.Is(err, errDenied) }},
				{Kind: Stale, Class: Correctable,
					Call: func(w world, row uuid.UUID) error {
						_, err := w.service.File(w.ctx, row, revisionOf(w, row)+1)
						return err
					},
					Is: func(_ world, err error) bool { return errors.Is(err, errStale) }},
				{Kind: Elsewhere,
					Call: func(w world, row uuid.UUID) error {
						_, err := w.service.File(w.elsewhere, row, revisionOf(w, row))
						return err
					},
					Is: func(_ world, err error) bool { return errors.Is(err, crud.ErrNotFound) }},
				{Name: "a sealed note is not filed twice", Class: Immutable,
					Provoke: func(t *testing.T, w world, _ uuid.UUID) uuid.UUID { return w.seed(t, true) },
					Call: func(w world, row uuid.UUID) error {
						_, err := w.service.File(w.ctx, row, revisionOf(w, row))
						return err
					},
					Is: func(_ world, err error) bool { return errors.Is(err, errSealed) }},
			},
		}},
		Own: []Case[world]{{
			Name:    "the note carries the day it was filed",
			Because: "what a success leaves behind is the port's domain, and no description reaches it",
			Run: func(t *testing.T, w world) {
				row := w.seed(t, false)
				if _, err := w.service.File(w.ctx, row, 0); err != nil {
					t.Fatalf("File: %v", err)
				}
				stored, err := w.service.Note(w.ctx, row)
				if err != nil {
					t.Fatalf("reading the note back: %v", err)
				}
				if !stored.UpdatedAt.Equal(w.service.Clock.Now()) {
					t.Errorf("the note was stamped %v and the clock reads %v", stored.UpdatedAt, w.service.Clock.Now())
				}
			},
		}},
	}
}

// revisionOf is the revision the caller says it read. A real module's closure
// reads it the same way, through the port.
func revisionOf(w world, row uuid.UUID) int64 {
	stored, err := w.service.Note(w.ctx, row)
	if err != nil {
		return 0
	}
	return stored.Rev
}
