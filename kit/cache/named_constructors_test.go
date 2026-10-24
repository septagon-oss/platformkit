package cache

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
)

func TestMemoryConstructorsKeepTheirKeysAndOwnTheirBackends(t *testing.T) {
	for _, app := range []string{"", "pkit", "billing-service", "1legacy", "1", strings.Repeat("a", 32)} {
		t.Run(app, func(t *testing.T) {
			constructors := []func() Cache{func() Cache { return MemoryFor(appname.Name(app)) }}
			if app != "" {
				constructors = append(constructors, func() Cache { return Memory(app) })
			}
			key := Of(uuid.MustParse("33333333-3333-4333-8333-333333333333"), "host").Entry("example.com")
			for _, build := range constructors {
				c := build()
				t.Cleanup(func() { _ = c.Close() })
				if err := c.Move(t.Context(), key.Scope()); err != nil {
					t.Fatal(err)
				}
				entry, err := GetEntry(t.Context(), c, key)
				if err != nil || entry.Found {
					t.Fatalf("initial read = %+v, %v", entry, err)
				}
				if err := c.Set(t.Context(), key, []byte("value"), time.Minute, entry.Generation); err != nil {
					t.Fatal(err)
				}
				if got, err := GetEntry(t.Context(), c, key); err != nil || !got.Found || string(got.Value) != "value" || got.Generation != entry.Generation {
					t.Fatalf("filled entry = %+v, %v", got, err)
				}
				backend := c.(*store).backend.(*memory)
				prefix := app + ":33333333-3333-4333-8333-333333333333:host"
				if got := backend.entries[prefix+"/example.com"].val; !bytes.Equal(got, []byte("1\nvalue")) {
					t.Errorf("physical entry = %q", got)
				}
				if got := backend.counters[prefix+"#"]; got != 1 {
					t.Errorf("physical generation = %d, want 1", got)
				}
				other := build()
				t.Cleanup(func() { _ = other.Close() })
				if got, err := GetEntry(t.Context(), other, key); err != nil || got.Found {
					t.Fatalf("independent store = %+v, %v", got, err)
				}
			}
		})
	}
}

func TestGetEntryKeepsMalformedMissesAndEmptyHits(t *testing.T) {
	for _, tc := range []struct {
		counter, raw, value string
		found               bool
		generation          Generation
	}{
		{"bad", "0\nstale", "", false, Generation{}},
		{"7", "bad", "", false, Generation{gen: 7, read: true}},
		{"7", "6\nstale", "stale", false, Generation{gen: 7, read: true}},
		{"0", "0\n", "", true, Generation{read: true}},
	} {
		backend := MemoryBackend()
		c, err := NewFor("", backend)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		for rawKey, rawValue := range map[string]string{"host#": tc.counter, "host/example.com": tc.raw} {
			if err := backend.Set(t.Context(), ":00000000-0000-0000-0000-000000000000:"+rawKey, []byte(rawValue), time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		got, err := GetEntry(t.Context(), c, Shared("host").Entry("example.com"))
		if err != nil || got.Found != tc.found || string(got.Value) != tc.value || got.Generation != tc.generation {
			t.Fatalf("counter %q, envelope %q: %+v, %v", tc.counter, tc.raw, got, err)
		}
	}
}

func TestCacheConstructorsShareValuesGenerationsAndClose(t *testing.T) {
	for _, app := range []string{"pkit", "1legacy"} {
		for _, named := range []bool{false, true} {
			backend := MemoryBackend()
			old, err := New(app, backend)
			if err != nil {
				t.Fatal(err)
			}
			if named {
				old = MemoryNamed(app, backend)
			}
			current, err := NewFor(appname.Name(app), backend)
			if err != nil {
				t.Fatal(err)
			}
			key := Shared("host").Entry("example.com")
			for _, pair := range [][2]Cache{{old, current}, {current, old}} {
				entry, err := GetEntry(t.Context(), pair[0], key)
				if err != nil {
					t.Fatal(err)
				}
				if err := pair[0].Set(t.Context(), key, []byte("shared"), time.Minute, entry.Generation); err != nil {
					t.Fatal(err)
				}
				if got, err := GetEntry(t.Context(), pair[1], key); err != nil || !got.Found || string(got.Value) != "shared" {
					t.Fatalf("cross read = %+v, %v", got, err)
				}
				if err := pair[0].Move(t.Context(), key.Scope()); err != nil {
					t.Fatal(err)
				}
				if got, err := GetEntry(t.Context(), pair[1], key); err != nil || got.Found || string(got.Value) != "shared" {
					t.Fatalf("closed generation = %+v, %v", got, err)
				}
			}
			if err := current.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := GetEntry(t.Context(), old, key); !errors.Is(err, ErrClosed) {
				t.Fatalf("shared Close = %v", err)
			}
		}
	}
}

func TestCacheConstructorsPreserveNameValidation(t *testing.T) {
	for _, app := range []string{"", "BAD", "a.b", "a/b", "a b", "-a", "a-", "a--b", strings.Repeat("a", 33)} {
		t.Run(app, func(t *testing.T) {
			_, want := Slug(app)
			if c, err := New(app, nil); c != nil || err == nil || err.Error() != want.Error() {
				t.Fatalf("legacy validation = %v, %v; want %v", c, err, want)
			}
			panics := []func(){func() { Memory(app) }, func() { MemoryNamed(app, nil) }}
			if app != "" {
				if c, err := NewFor(appname.Name(app), nil); c != nil || err == nil || err.Error() != want.Error() {
					t.Fatalf("typed validation = %v, %v; want %v", c, err, want)
				}
				panics = append(panics, func() { MemoryFor(appname.Name(app)) })
			}
			for _, build := range panics {
				func() {
					defer func() {
						got, ok := recover().(error)
						if !ok || got.Error() != want.Error() {
							t.Errorf("panic = %v; want %v", got, want)
						}
					}()
					build()
				}()
			}
		})
	}
	for _, app := range []appname.Name{"", "pkit", "1legacy"} {
		if c, err := NewFor(app, nil); err != nil || c == nil {
			t.Fatalf("constructor probed backend: %v, %v", c, err)
		}
	}
}

type entryReader struct {
	Cache
	read func(context.Context, Key) ([]byte, bool, Generation, error)
}

func (r entryReader) Get(ctx context.Context, key Key) ([]byte, bool, Generation, error) {
	return r.read(ctx, key)
}

func TestGetEntryForwardsOneReadUnchanged(t *testing.T) {
	key := Shared("host").Entry("example.com")
	ctx := t.Context()
	under := Generation{gen: 7, read: true}
	boom := errors.New("backend unavailable")
	for _, found := range []bool{false, true} {
		for _, value := range [][]byte{nil, {}, {0, 255, 1}} {
			for _, wantErr := range []error{nil, boom} {
				calls := 0
				c := entryReader{read: func(gotCtx context.Context, gotKey Key) ([]byte, bool, Generation, error) {
					calls++
					if gotCtx != ctx || gotKey != key {
						t.Fatal("read changed context or key")
					}
					return value, found, under, wantErr
				}}
				got, err := GetEntry(ctx, c, key)
				if calls != 1 || err != wantErr || got.Found != found || got.Generation != under || !bytes.Equal(got.Value, value) || (got.Value == nil) != (value == nil) {
					t.Fatalf("read = %+v, %v; calls=%d", got, err, calls)
				}
				if len(value) > 0 && &got.Value[0] != &value[0] {
					t.Error("read copied the caller's bytes")
				}
			}
		}
	}
}
