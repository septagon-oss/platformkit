package app

// cache_test.go walks the one choice kit/app makes about where a shared value
// lives, which is the choice no other case in this repository reaches.
//
// The reference composition boots the in-process store: cache.adapter is empty
// there, so nothing that boots apps/platformkit can see the branch that names a
// shared one. The shared branch is Options.Caches.Valkey, wired in
// apps/platformkit/modules.go to valkey.Connect — and kit/app links no cache
// client of its own (scripts/check_packages.sh holds that line), so the only
// thing this package can be tested on is the selection itself: the settings that
// name the store, the constructor it calls, what a constructor that fails or
// answers nothing does to the boot, and who releases the store it got. That is
// exactly the surface TestTheTransportConstructorMustAnswerWithATransport and
// TestCloseReleasesThePoolOnce hold for the transport, run against the store.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/module"
)

// sharedCache is the store a test's constructor hands back, counting what the
// Runtime did with it — lifecycle_test.go's countingTransport, for the cache.
type sharedCache struct {
	cache.Cache
	closes int
}

func (c *sharedCache) Close() error {
	c.closes++
	return c.Cache.Close()
}

// TestTheSharedStoreIsTheOneTheDeploymentNamed is the selection walked end to
// end: cache.adapter valkey makes Start call the composition's constructor once,
// with the settings the boot validated, and makes Close the one release of what
// it returned. A store that survived Close would hold a connection open past the
// Runtime that owns it, which is the same fault the pool and the transport are
// held to.
func TestTheSharedStoreIsTheOneTheDeploymentNamed(t *testing.T) {
	cfg, opts := compose(t)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "acme-stack", URL: "valkey://127.0.0.1:6379", Password: "s3cret"}
	store := &sharedCache{Cache: cache.Memory("acme-stack")}
	built := 0
	var handed config.Cache
	opts.Caches = Caches{Valkey: func(_ context.Context, c config.Cache) (cache.Cache, error) {
		built++
		handed = c
		return store, nil
	}}
	a, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if built != 1 {
		t.Errorf("the shared store was built %d times, want the one call Start owns", built)
	}
	if handed != cfg.Cache {
		t.Errorf("the constructor was handed %+v, want the settings the boot validated: %+v", handed, cfg.Cache)
	}
	if err := rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for range 2 {
		if err := rt.Close(); err != nil {
			t.Errorf("Close again = %v, want the first call's result", err)
		}
	}
	if store.closes != 1 {
		t.Errorf("the shared store was closed %d times, want once", store.closes)
	}
}

// TestStartRefusesAStoreItNamedAndCannotBuild is the boot that refuses rather
// than cold-starts: a deployment that set cache.adapter valkey and gets an
// in-process store anyway has lost every invalidation it will ever run, so both
// ways a constructor can fail — the store is down, and the constructor answers
// (nil, nil) — refuse Start with no Runtime.
func TestStartRefusesAStoreItNamedAndCannotBuild(t *testing.T) {
	for _, test := range []struct {
		name  string
		build func(context.Context, config.Cache) (cache.Cache, error)
		want  string
	}{
		{"down", func(context.Context, config.Cache) (cache.Cache, error) {
			return nil, errors.New("store is down")
		}, "store is down"},
		{"silent", func(context.Context, config.Cache) (cache.Cache, error) {
			return nil, nil
		}, "answered with no store and no error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg, opts := compose(t)
			cfg.Cache = config.Cache{Adapter: "valkey", App: "acme-stack", URL: "valkey://127.0.0.1:6379"}
			opts.Caches = Caches{Valkey: test.build}
			a, err := New(t.Context(), cfg, []module.Module{brand("alpha", "alpha-stylesheet")}, opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			rt, err := a.Start(t.Context())
			if rt != nil {
				_ = rt.Close()
				t.Fatal("Start handed back a Runtime whose store it never built")
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Start = %v, want the failure that names %q", err, test.want)
			}
			if !strings.Contains(err.Error(), "cache.adapter valkey") {
				t.Errorf("Start = %v, want it to name the setting the operator has to edit", err)
			}
		})
	}
}

// TestNewRefusesAMissingCacheConstructor: the kernel knows the one shared name
// and builds none of the stores behind it, so a composition that deploys with
// cache.adapter valkey and no constructor is refused before anything opens — in
// every role, because the worker half must answer "will this start?" the way the
// web half does.
func TestNewRefusesAMissingCacheConstructor(t *testing.T) {
	for _, role := range []Role{All, Web, Worker} {
		cfg, opts := compose(t)
		cfg.Cache = config.Cache{Adapter: "valkey", App: "acme-stack", URL: "valkey://127.0.0.1:6379"}
		opts.Caches = Caches{}
		opts.Role = role
		if _, err := New(t.Context(), cfg, nil, opts); err == nil || !strings.Contains(err.Error(), "Caches.Valkey") {
			t.Errorf("New role %s = %v, want the refusal naming Caches.Valkey", role, err)
		}
	}
}

// TestACacheAppThatIsNotASlugRefusesTheBootBeforeTheStore is the ordering of two
// refusals: the name every shared key begins with is checked before the store is
// opened, so a bad cache.app costs no connection and no half-built store.
func TestACacheAppThatIsNotASlugRefusesTheBootBeforeTheStore(t *testing.T) {
	cfg, opts := compose(t)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "Acme Stack", URL: "valkey://127.0.0.1:6379"}
	built := false
	opts.Caches = Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) {
		built = true
		return cache.Memory("acme-stack"), nil
	}}
	a, err := New(t.Context(), cfg, nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := a.cache(t.Context()); err == nil || !strings.Contains(err.Error(), "cache.app") {
		t.Fatalf("cache = %v, want the refusal naming cache.app", err)
	}
	if built {
		t.Error("the store was built over an application name no key can carry")
	}
}

// TestTheInProcessStoreSaysSoOnceAndReachesOneProcess is the other half of the
// selection, and the sentence an operator reads: with no cache.adapter the boot
// builds the store of this process and says out loud that a value forgotten here
// is forgotten here only — and the two processes that read that sentence really
// are two, so a suspension run through one is a miss for the other.
func TestTheInProcessStoreSaysSoOnceAndReachesOneProcess(t *testing.T) {
	log := &bytes.Buffer{}
	first, err := inProcessApp(t, log).cache(t.Context())
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	second, err := inProcessApp(t, &bytes.Buffer{}).cache(t.Context())
	if err != nil {
		t.Fatalf("cache: %v", err)
	}
	key := cache.Shared("hosts").Entry("acme.test")
	if err := first.Set(t.Context(), key, []byte("acme"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, found, err := first.Get(t.Context(), key); err != nil || !found {
		t.Fatalf("Get through the store that wrote it = found %v, err %v, want the value back", found, err)
	}
	if _, found, err := second.Get(t.Context(), key); err != nil || found {
		t.Fatalf("Get through a second process's store = found %v, err %v, want a miss: an in-process store reaches one process", found, err)
	}
	line := log.String()
	if !strings.Contains(line, "in-process store") {
		t.Errorf("the boot logged %q, want the sentence that says a forgotten value reaches this process only", strings.TrimSpace(line))
	}
	if strings.Count(line, "in-process store") != 1 {
		t.Errorf("the boot said it %d times, want once", strings.Count(line, "in-process store"))
	}
}

// inProcessApp is a composition with no cache section — the shape every
// installation has until an operator names a shared store — with its log going
// to buf so the sentence it boots with can be read rather than assumed.
func inProcessApp(t *testing.T, buf *bytes.Buffer) *App {
	t.Helper()
	cfg, opts := compose(t)
	opts.Log = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	a, err := New(t.Context(), cfg, nil, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}
