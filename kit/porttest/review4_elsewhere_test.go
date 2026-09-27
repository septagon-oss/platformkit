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

// Review 4's pin, on the tenant case. README.md's table says case 6 asserts the
// shared floor "and the row is still there for the tenant that owns it", and
// refused() says the same in its own words. The guard is
// `r.Kind == Elsewhere && op.Snapshot != nil && snapshot() == ""` — keyed on the
// snapshot rendering the empty string. Provoke's documented job is to move the
// world into the state the refusal answers; a description that switches tenant
// there reads its Snapshot through the visitor, where the row was never visible:
// before and after both render the same nothing, the unchanged check passes, and
// the guard cannot fire because "no such box" is not "". The case is green while
// the owner has no row. The fix must ask whether the row is still there some way
// that does not depend on what a Snapshot renders for a missing row — no Snapshot
// in this repository renders "" ("no such note", "no such task", "no settings").

type box struct {
	entity.Base
	Label  string
	Placed bool
}

func (box) TableName() string { return "boxes" }

func (b *box) Validate(context.Context) error {
	if b.Label == "" {
		return fmt.Errorf("%w: a box has a label", crud.ErrInvalid)
	}
	return nil
}

const placePermission = "shelf.place"

// shelf is the one-command port. erases is the bug the tenant case exists to
// catch: refused as another tenant's, and takes the row away from its owner.
type shelf struct {
	*Fake
	rows   *Store[box]
	home   tenancy.Tenant
	erases bool
}

func newShelf(erases bool, home tenancy.Tenant) *shelf {
	return &shelf{
		Fake: NewFake(time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC), []string{"box.placed"}),
		rows: NewStore(func(b box) uuid.UUID { return b.ID }), home: home, erases: erases,
	}
}

func (s *shelf) Place(ctx context.Context, row uuid.UUID) (box, error) {
	if s.erases && tenantOf(ctx) != s.home.ID {
		s.rows.Delete(tenancy.WithTenant(context.Background(), s.home), row)
		return box{}, fmt.Errorf("%w: no such box", crud.ErrNotFound)
	}
	return Do(ctx, s.Fake, s.rows, Command[box]{ // idempotent, so the retry is asked
		Permission: placePermission, Denied: errDenied, Row: row,
		Apply: func(b *box) []string {
			if b.Placed {
				return nil
			}
			b.Placed = true
			return []string{"box.placed"}
		},
	})
}

func (s *shelf) Note(ctx context.Context, row uuid.UUID) (box, error) { return s.rows.Get(ctx, row) }

type shelfWorld struct {
	ctx, elsewhere context.Context
	service        *shelf
	seed           func(t *testing.T) uuid.UUID
}

func shelfWorldFn(erases bool) func(*testing.T, func(*shelfWorld)) {
	return func(t *testing.T, run func(*shelfWorld)) {
		t.Helper()
		here := tenancy.Tenant{ID: uuid.New(), Slug: "here"}
		there := tenancy.Tenant{ID: uuid.New(), Slug: "there"}
		keeper := uuid.New()
		service := newShelf(erases, here)
		service.Grants.Grant(keeper, placePermission)
		ctx := tenancy.WithTenant(tenancy.WithActor(context.Background(), keeper), here)
		w := &shelfWorld{ctx: ctx, service: service,
			elsewhere: tenancy.WithTenant(tenancy.WithActor(context.Background(), keeper), there)}
		w.seed = func(t *testing.T) uuid.UUID {
			t.Helper()
			return Seed(ctx, service.Fake, service.rows, box{Label: "the winter coats"})
		}
		run(w)
	}
}

// shelfSuite describes one mutating command and the floor it owes; Stale is
// declined in writing, as a module's description does.
func shelfSuite(erases bool) Suite[*shelfWorld] {
	placed := func(w *shelfWorld, row uuid.UUID) (string, error) {
		got, err := w.service.Place(w.ctx, row)
		return fmt.Sprintf("placed=%v label=%q", got.Placed, got.Label), err
	}
	unknown := func(w *shelfWorld, _ uuid.UUID) error {
		_, err := w.service.Place(w.ctx, uuid.New())
		return err
	}
	elsewhere := func(w *shelfWorld, row uuid.UUID) error { _, err := placed(w, row); return err }
	notFound := func(_ *shelfWorld, err error) bool { return errors.Is(err, crud.ErrNotFound) }
	return Suite[*shelfWorld]{
		Port:     "shelf.Service",
		World:    shelfWorldFn(erases),
		Events:   func(w *shelfWorld) []string { return w.service.Events.Names() },
		Classify: func(error) Class { return Unclassified },
		Ops: []Op[*shelfWorld]{{
			Name: "Place", Mutates: true, Publishes: []string{"box.placed"},
			Ready: func(t *testing.T, w *shelfWorld) uuid.UUID {
				t.Helper()
				return w.seed(t)
			},
			Call: placed,
			Snapshot: func(t *testing.T, w *shelfWorld, row uuid.UUID) string {
				t.Helper()
				stored, err := w.service.Note(w.ctx, row)
				if err != nil {
					return "no such box"
				}
				return fmt.Sprintf("placed=%v label=%q", stored.Placed, stored.Label)
			},
			Refusals: []Refusal[*shelfWorld]{
				{Kind: Unknown, Call: unknown, Is: notFound},
				{Kind: Denied, Call: func(w *shelfWorld, row uuid.UUID) error {
					_, err := w.service.Place(tenancy.WithActor(w.ctx, uuid.New()), row)
					return err
				}, Is: func(_ *shelfWorld, err error) bool { return errors.Is(err, errDenied) }},
				{Kind: Elsewhere, Provoke: func(_ *testing.T, w *shelfWorld, row uuid.UUID) uuid.UUID {
					w.ctx = w.elsewhere // the world moves, as Provoke is for
					return row
				}, Call: elsewhere, Is: notFound},
			},
			Skip: map[Kind]string{Stale: "a box carries no revision; kit/crud's PATCH owns that check"},
		}},
	}
}

func runShelf(t *testing.T, erases bool) *transcript {
	t.Helper()
	log := &transcript{failed: map[string][]string{}}
	run(watcher{t: t, log: log}, shelfSuite(erases))
	return log
}

// TestTheErasingRefusalIsReached is the reachability probe, blind to the harness:
// through the port and the store only — the call was refused, and the owner's row
// is gone. It passes today, so the case below is not vacuous.
func TestTheErasingRefusalIsReached(t *testing.T) {
	here := tenancy.Tenant{ID: uuid.New(), Slug: "here"}
	there := tenancy.Tenant{ID: uuid.New(), Slug: "there"}
	keeper := uuid.New()
	service := newShelf(true, here)
	service.Grants.Grant(keeper, placePermission)
	ctx := tenancy.WithTenant(tenancy.WithActor(context.Background(), keeper), here)
	row := Seed(ctx, service.Fake, service.rows, box{Label: "the winter coats"})
	_, err := service.Place(tenancy.WithTenant(tenancy.WithActor(context.Background(), keeper), there), row)
	if !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("the other tenant's call answered %v; the description names ErrNotFound as the refusal", err)
	}
	if _, err := service.Note(ctx, row); !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("the owner reads %v after the refused call; the defect is that the row is gone", err)
	}
}

// TestRunFailsTheTenantCaseWhenTheRefusalTookTheRowAway is the assertion README.md
// makes and the code does not keep. TestRunPassesTheShelfWhenNothingIsBroken is
// its control: the description is sound, and only the knob moves the result.
func TestRunFailsTheTenantCaseWhenTheRefusalTookTheRowAway(t *testing.T) {
	const name = "Place: " + string(Elsewhere)
	log := runShelf(t, true)
	if said := log.failures(name); len(said) == 0 {
		t.Fatalf("%q passed although the refused call took the row away from its owner; it ran %v, and README.md says case 6 asserts the row is still there", name, log.names())
	}
	log.mustPassApartFrom(t, name)
}

func TestRunPassesTheShelfWhenNothingIsBroken(t *testing.T) {
	log := runShelf(t, false)
	if len(log.names()) == 0 {
		t.Fatal("the shelf description ran no case at all")
	}
	log.mustPassApartFrom(t)
}
