package cachetest

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// C1. A miss is a fact and not an outage. An adapter that surfaces "no such
// key" as an error makes every cold cache look like a dead one, and the host
// resolution that fails open on an error then fails open on every request.
func miss(t *testing.T, store Store) {
	c := fresh(t, store)
	entry, err := cache.GetEntry(t.Context(), c, cache.Shared("host").Entry("nobody.example"))
	if err != nil || entry.Found || entry.Value != nil {
		t.Fatalf("Get of an absent key = %+v, err=%v; want nothing, false, no error", entry, err)
	}
}

// C2. Byte for byte. An adapter that coerces the value through a type that
// cannot hold a zero byte, or encodes on one side and not the other, corrupts
// the envelope and the resolution comes back as nothing.
func valueBytes(t *testing.T, store Store) {
	c := fresh(t, store)
	want := []byte{0x00, 0xff, 0xfe, 'a', 0x00, 0x7f, 0xc3, 0x28}
	key := cache.Shared("host").Entry("example.com")
	if err := fill(t, c, key, want, time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	entry, err := cache.GetEntry(t.Context(), c, key)
	if err != nil || !entry.Found {
		t.Fatalf("Get: found=%v err=%v", entry.Found, err)
	}
	if !bytes.Equal(entry.Value, want) {
		t.Errorf("Get = % x; want % x", entry.Value, want)
	}
}

// C3. An entry with no lifetime is refused. The measured default of a write
// with no expiry is "forever" — the TTL of such a key answers -1s — so an
// adapter that let it through would be holding a stale answer nobody owns.
func requireLifetime(t *testing.T, store Store) {
	c := fresh(t, store)
	key := cache.Shared("host").Entry("example.com")
	_, _, under, err := c.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("reading the generation to write under: %v", err)
	}
	for _, ttl := range []time.Duration{0, -time.Second} {
		err := c.Set(context.Background(), key, []byte("x"), ttl, under)
		if !errors.Is(err, cache.ErrNoLifetime) {
			t.Errorf("Set with ttl %s = %v; want ErrNoLifetime", ttl, err)
		}
		if _, found, _, err := c.Get(context.Background(), key); err != nil || found {
			t.Errorf("the refused Set wrote something: found=%v err=%v", found, err)
		}
	}
}

// C4. An entry expires. An adapter that ignores the lifetime never forgets a
// suspended host, and the lifetime is the only thing standing between a failed
// invalidation and a permanent one. Wide margins: this is the store's clock,
// and no fake reaches it.
func expires(t *testing.T, store Store) {
	c := fresh(t, store)
	key := cache.Shared("host").Entry("example.com")
	if err := fill(t, c, key, []byte("x"), 60*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, _, err := c.Get(context.Background(), key); err != nil || !found {
		t.Fatalf("the entry was not there to begin with: found=%v err=%v", found, err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, found, _, err := c.Get(context.Background(), key); err != nil || found {
		t.Errorf("the entry outlived its lifetime: found=%v err=%v", found, err)
	}
}
