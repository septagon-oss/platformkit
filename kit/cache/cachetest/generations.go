package cachetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// Generations runs the port's own invalidation rules against a store the suite
// controls, so the two things a racing write turns on can be observed instead of
// raced for: which generation it was stamped by, and how many commands it cost the
// store to say so.
//
// It runs wherever kit/cache's store runs — both adapters share that code — and it
// is the case a delete-only adapter passes today and must not.
func Generations(t *testing.T) {
	t.Run("a write stamped by the read before a move is not believed", fillAcrossMove)

	t.Run("a write stamped by no read is refused and writes nothing", func(t *testing.T) {
		backend := &scripted{entries: map[string]stored{}, counters: map[string]int64{}}
		c, err := cache.NewFor("pkit", backend)
		if err != nil {
			t.Fatalf("NewFor: %v", err)
		}
		ctx := context.Background()
		key := cache.Shared("host").Entry("acme.example")

		// A Generation is not a number a caller may reach for. A write that names
		// no read can only be stamped with the open generation at the moment of
		// the write, which is the one stamp that survives a move that already
		// happened — so it is refused rather than guessed at, and it writes nothing.
		reads, writes := backend.commands()
		if err := c.Set(ctx, key, []byte("a guess"), time.Minute, cache.Generation{}); !errors.Is(err, cache.ErrNoGeneration) {
			t.Errorf("Set under no read = %v; want ErrNoGeneration", err)
		}
		if r, w := backend.commands(); r != reads || w != writes {
			t.Errorf("the refused write cost the store %d reads and %d writes", r-reads, w-writes)
		}
		if _, found, _, err := c.Get(ctx, key); err != nil || found {
			t.Errorf("the refused write stored something: found=%v err=%v", found, err)
		}
	})

	t.Run("two concurrent moves are two moves and neither is lost", func(t *testing.T) {
		backend := &scripted{entries: map[string]stored{}, counters: map[string]int64{}}
		c, err := cache.NewFor("pkit", backend)
		if err != nil {
			t.Fatalf("NewFor: %v", err)
		}
		ctx := context.Background()
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := c.Move(ctx, cache.Shared("host")); err != nil {
					t.Errorf("Move: %v", err)
				}
			}()
		}
		wg.Wait()
		if got := backend.counter("host#"); got != 8 {
			t.Errorf("eight moves left the generation at %d; a lost move is a suspension nobody honoured", got)
		}
	})
}

func fillAcrossMove(t *testing.T) {
	backend := &scripted{entries: map[string]stored{}, counters: map[string]int64{}}
	c, err := cache.NewFor("pkit", backend)
	if err != nil {
		t.Fatalf("NewFor: %v", err)
	}
	reader, err := cache.NewFor("pkit", backend)
	if err != nil {
		t.Fatalf("NewFor: %v", err)
	}
	ctx := t.Context()
	key := cache.Shared("host").Entry("acme.example")

	// Replica B misses, and the miss hands it the generation it missed under.
	// It goes to the loader — the slow part, the one a suspension overtakes —
	// and replica A commits the suspension and moves the Scope while B is away.
	// Nothing orders these two any more: the stamp is the synchronisation.
	reads, writes := backend.commands()
	entry, err := cache.GetEntry(ctx, reader, key)
	if err != nil || entry.Found {
		t.Fatalf("the read that decided the load: found=%v err=%v", entry.Found, err)
	}
	if r, w := backend.commands(); r != reads+1 || w != writes {
		t.Fatalf("one GetEntry cost %d reads and %d writes", r-reads, w-writes)
	}
	if err := c.Move(ctx, cache.Shared("host")); err != nil {
		t.Fatalf("Move during the racing load: %v", err)
	}

	// B writes what it loaded. The port neither refuses nor warns: the entry
	// carries a closed generation, which is the state a delete left it in, and
	// it expires on the lifetime this call was made with.
	reads, writes = backend.commands()
	if err := c.Set(ctx, key, []byte("the tenant as it was before the suspension"), time.Minute, entry.Generation); err != nil {
		t.Fatalf("the racing Set: %v", err)
	}
	// The stamp travelled for free. A write that asked the store what is open
	// would cost one more command on every fill — and would stamp this entry
	// with the generation that just opened, which is the resurrection below.
	if r, w := backend.commands(); r != reads || w != writes+1 {
		t.Errorf("one fill cost %d reads and %d writes; want no read and one write", r-reads, w-writes)
	}

	if got, err := cache.GetEntry(ctx, reader, key); err != nil || got.Found || string(got.Value) != "the tenant as it was before the suspension" {
		t.Fatalf("the closed generation's bytes were not preserved as a miss: %+v, %v", got, err)
	}
	// And the entry expires on its own: the resurrection is bounded even where
	// the invalidation is missed, which is why every Set carries a lifetime.
	if _, ok := backend.expiry(key.String()); !ok {
		t.Error("the racing write was stored with no expiry")
	}
}
