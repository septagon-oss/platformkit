package porttest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Do says of itself that it "holds the store for the whole command — the fake's
// stand-in for the row lock". Nothing in this package ever ran two commands at
// one row, so that sentence described a mutex nobody had watched: move the lock
// from Do into Store.get and Store.put — load and write still work, every
// existing case still passes — and the fake starts answering the sweep the way
// two database transactions do, which is twice.
//
// The shape is the one the row lock exists for. Every caller read the note at
// revision 0, so exactly one of them is first: it files the note and says so;
// the rest are refused a revision the row is not at, and write and say nothing.
// One filing, one event, one revision, whatever the order.
func TestTwoCallersAtOneRowFileItOnce(t *testing.T) {
	const callers = 8
	ctx := tenancy.WithTenant(tenancy.WithActor(context.Background(), uuid.New()),
		tenancy.Tenant{ID: uuid.New(), Slug: "here"})

	// Reachability, in its own world: one caller at the revision it read does
	// file the note and does say so. Without this leg the counts below would be
	// answered just as well by a command that never runs.
	single := NewFake(clockAt(), []string{"note.filed"})
	singleStore := NewStore(func(n note) uuid.UUID { return n.ID })
	singleRow := Seed(ctx, single, singleStore, note{Title: "the minutes"})
	if _, err := file(ctx, single, singleStore, singleRow, 0); err != nil {
		t.Fatalf("one caller at revision 0: %v", err)
	}
	if got := single.Events.Count(); got != 1 {
		t.Fatalf("the first File published %d events, want 1", got)
	}

	// Eight rounds, because a race that loses the lock answers the first round
	// correctly more often than not. Each round is a fresh note and a fresh
	// fake, so no round reads another's rows.
	for round := range 8 {
		fake := NewFake(clockAt(), []string{"note.filed"})
		store := NewStore(func(n note) uuid.UUID { return n.ID })
		row := Seed(ctx, fake, store, note{Title: "the minutes"})

		var wg sync.WaitGroup
		var mu sync.Mutex
		accepted, refused := 0, 0
		for range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Every caller holds the revision it read before anybody wrote:
				// the sweep started from one list, and the sweep is what runs
				// this command every minute forever.
				if _, err := file(ctx, fake, store, row, 0); err != nil {
					mu.Lock()
					refused++
					mu.Unlock()
					if !errors.Is(err, errStale) {
						t.Errorf("round %d: a caller whose revision has moved was refused with %v, want %v", round, err, errStale)
					}
					return
				}
				mu.Lock()
				accepted++
				mu.Unlock()
			}()
		}
		wg.Wait()

		if accepted != 1 {
			t.Errorf("round %d: %d of %d callers filed the note; one of them was first, and the rest are owed %v",
				round, accepted, callers, errStale)
		}
		if got := fake.Events.Count(); got != 1 {
			t.Errorf("round %d: %d callers filed one note and the fake said it %d times; a subscriber told twice about one filing is the reason Do holds the store",
				round, callers, got)
		}
		stored, err := store.Get(ctx, row)
		if err != nil {
			t.Fatalf("round %d: Get: %v", round, err)
		}
		if stored.Rev != 1 {
			t.Errorf("round %d: the note sits at revision %d after %d callers, want 1", round, stored.Rev, callers)
		}
	}
}

// TestDoHoldsTheStoreWhileTheCommandDecides is the same claim read from the inside, and
// it is the one that discriminates: the behavioural test above passes by luck
// under a Do that dropped the lock, because the window is nanoseconds wide.
// Ask the store whether it is held at the moment the decision runs, and the
// answer is not probabilistic.
//
// The mutex is this package's own, so the test reaches for it deliberately:
// "Do holds the store for the whole command" is a sentence about that mutex and
// nothing else can be read for it.
func TestDoHoldsTheStoreWhileTheCommandDecides(t *testing.T) {
	ctx := tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: uuid.New(), Slug: "here"})
	fake := NewFake(clockAt(), []string{"note.filed"})
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	row := Seed(ctx, fake, store, note{Title: "the minutes"})

	asked := false
	_, err := Do(ctx, fake, store, Command[note]{
		Row: row,
		Apply: func(n *note) []string {
			asked = true
			if store.mu.TryLock() {
				store.mu.Unlock()
				t.Error("the store was free while the command was deciding; load, decide and write are one step here, and one the fake cannot break between readers")
				return nil
			}
			n.Filed, n.Rev = true, n.Rev+1
			return []string{"note.filed"}
		},
	})
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if !asked {
		t.Fatal("the command never decided, so the assertion above ran against nothing")
	}
	if got := fake.Events.Count(); got != 1 {
		t.Errorf("the command published %d events, want 1", got)
	}
}

// The same row, reached from two tenants at once. The partition holds under
// contention, which the sequential tenant tests cannot establish: a map keyed
// under a lock nobody contends is untested in exactly the way row-level security
// would be if nobody ever opened a second session.
func TestTwoTenantsAtOneRowNeverShare(t *testing.T) {
	here := tenancy.Tenant{ID: uuid.New(), Slug: "here"}
	there := tenancy.Tenant{ID: uuid.New(), Slug: "there"}
	ctxHere := tenancy.WithTenant(context.Background(), here)
	ctxThere := tenancy.WithTenant(context.Background(), there)

	fake := NewFake(clockAt(), []string{"note.filed"})
	store := NewStore(func(n note) uuid.UUID { return n.ID })
	row := Seed(ctxHere, fake, store, note{Title: "the minutes"})

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := store.Get(ctxHere, row); err != nil {
				t.Errorf("the owning tenant read its own row: %v", err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := store.Get(ctxThere, row); err == nil {
				t.Error("another tenant read the row; a row somebody else holds is not found either")
			}
			// The write door is the same partition: a command that wrote into a
			// tenant that never had the row would take it away from its owner.
			if _, err := file(ctxThere, fake, store, row, 0); err == nil {
				t.Error("another tenant's File answered no error at all")
			}
		}()
	}
	wg.Wait()

	if got := len(store.All(ctxThere)); got != 0 {
		t.Errorf("the other tenant holds %d rows", got)
	}
	stored, err := store.Get(ctxHere, row)
	if err != nil {
		t.Fatalf("the owning tenant lost its row: %v", err)
	}
	if stored.Filed {
		t.Error("another tenant filed the note")
	}
	if got := fake.Events.Count(); got != 0 {
		t.Errorf("the events recorded are %d, want none: a call into another tenant's partition is not news", got)
	}
}

// file is the port's command as a module's fake hands it: the caller's own
// revision and nothing else. notes_test.go's File reads the row twice for its
// knobs, which is not what a lock case wants.
func file(ctx context.Context, f *Fake, s *Store[note], row uuid.UUID, expected int64) (note, error) {
	return Do(ctx, f, s, Command[note]{
		Row:      row,
		Expected: expected,
		Revision: func(n note) int64 { return n.Rev },
		Stale: func(stored, at int64) error {
			return errStale
		},
		Apply: func(n *note) []string {
			if n.Filed {
				return nil
			}
			n.Filed, n.Rev = true, n.Rev+1
			return []string{"note.filed"}
		},
	})
}

func clockAt() time.Time { return time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC) }
