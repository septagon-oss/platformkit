// The adapter's own test. It is the conformance suite first — the same cases the
// in-process store runs, for the same reasons — and then the two claims only a
// server can answer: what a thousand-key invalidation costs in commands, and what
// a lookup costs.
package valkey_test

import (
	"context"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
	"github.com/septagon-oss/platformkit/kit/cache/providers/valkey"
	"github.com/septagon-oss/platformkit/kit/config"
)

// unreachable is an address with nothing behind it: the store the fail-open cases
// are about. Port 1 on localhost refuses, which is an answer, not a hang.
const unreachable = "redis://127.0.0.1:1"

// TestTheSharedStoreIsACache runs the one harness every adapter of cache.Cache
// runs against a real Valkey.
//
// It reads PLATFORMKIT_TEST_VALKEY_URL — `make up` starts the `valkey` service the
// Makefile points that variable at. With no address set the case skips, and the
// sentence says why that departs from `make test`'s stance on NATS: the worker
// transport every journey exercises has to be there, while a cache server is
// optional infrastructure this kernel boots without, because the in-process store
// is a complete adapter. Nothing the Done-when claims about a second process
// depends on this run: the sharing and the invalidation are proved over the port
// in kit/cache's own test and in kit/httpx's two-API case, in `make check`,
// unconditionally (SPECIFY Limits L7). What this run adds is that the four
// commands really are what a RESP server answers.
func TestTheSharedStoreIsACache(t *testing.T) {
	url := address(t)
	store := func(t *testing.T) cache.Cache { return connect(t, url, run(t)) }
	failing := func(t *testing.T) cache.Cache { return lazy(t, unreachable, run(t)) }
	cachetest.Conformance(t, store, failing)
	cachetest.Groups(t, store, failing)
}

// TestARequestCostsOneRoundTrip is the cost side of the port: a hit is one MGET,
// which is why the generation and the entry are read together rather than in
// sequence, and a move is one INCR of a named key rather than a scan of a prefix.
//
// The commands are counted on the server, from INFO commandstats, because the
// adapter holds no counter of its own and a test-only seam to count them would be
// a seam no production code calls.
func TestARequestCostsOneRoundTrip(t *testing.T) {
	url, app := address(t), run(t)
	c := connect(t, url, app)
	ctx := context.Background()
	scope := cache.Of(uuid.New(), "resolution")
	key := scope.Entry("acme.example")

	// The write is stamped by a read, as every write is; the MGET below counts
	// only what the lookup under test costs.
	_, _, under, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get before the write: %v", err)
	}
	sets, mgetsBefore := commands(t, url, "set"), commands(t, url, "mget")
	if err := c.Set(ctx, key, []byte("acme"), time.Minute, under); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// One fill is one command. A Set that asked the store which generation is
	// open would be a second one, and would stamp the entry with the generation
	// that moved since the read that decided the load.
	if got := commands(t, url, "set") - sets; got != 1 {
		t.Errorf("one write cost %d SET commands; want 1", got)
	}
	if got := commands(t, url, "mget") - mgetsBefore; got != 0 {
		t.Errorf("one write cost %d MGET commands; want 0 — the generation travels with the read, not with the write", got)
	}
	mgets := commands(t, url, "mget")
	incrBefore := commands(t, url, "incr")
	if _, found, _, err := c.Get(ctx, key); err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got := commands(t, url, "mget") - mgets; got != 1 {
		t.Errorf("one read cost %d MGET commands; want 1 — the generation and the entry are one command", got)
	}
	if err := c.Move(ctx, scope); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if got := commands(t, url, "incr") - incrBefore; got != 1 {
		t.Errorf("one move cost %d INCR commands; want 1 — a move closes a generation, it does not sweep a prefix", got)
	}
	if _, found, _, err := c.Get(ctx, key); err != nil || found {
		t.Errorf("Get after Move = found=%v err=%v; want a miss", found, err)
	}
}

// TestDeletingAThousandKeysIsTwoCommands is the batching C9 cannot see: what one
// invalidation of a tenant's whole host set costs on the wire. A suspension
// invalidates every host the tenant has, and a delete that went one key per
// command would make the operator's action cost as many round trips as the
// tenant has hosts.
func TestDeletingAThousandKeysIsTwoCommands(t *testing.T) {
	url, app := address(t), run(t)
	c := connect(t, url, app)
	ctx := context.Background()
	scope := cache.Of(uuid.New(), "hosts")

	_, _, under, err := c.Get(ctx, scope.Entry("host-0.example"))
	if err != nil {
		t.Fatalf("Get before the writes: %v", err)
	}
	keys := make([]cache.Key, 0, 1000)
	for i := range 1000 {
		k := scope.Entry("host-" + strconv.Itoa(i) + ".example")
		if err := c.Set(ctx, k, []byte("v"), time.Minute, under); err != nil {
			t.Fatalf("Set: %v", err)
		}
		keys = append(keys, k)
	}
	before := commands(t, url, "del")
	if err := c.Delete(ctx, keys...); err != nil {
		t.Fatalf("Delete of 1000 keys: %v", err)
	}
	const want = 2 // deleteChunk is 500; 1000 keys are two commands and not a thousand
	if got := commands(t, url, "del") - before; got != want {
		t.Errorf("deleting 1000 keys cost %d DEL commands; want %d", got, want)
	}
	for _, k := range keys[:10] {
		if _, found, _, err := c.Get(ctx, k); err != nil || found {
			t.Fatalf("Get after Delete: found=%v err=%v", found, err)
		}
	}
}

// address is the store under test, or the named skip.
func address(t *testing.T) string {
	t.Helper()
	url := os.Getenv("PLATFORMKIT_TEST_VALKEY_URL")
	if url == "" {
		t.Skip("PLATFORMKIT_TEST_VALKEY_URL is unset; `make up` starts the valkey service this case reads (see the skip sentence on TestTheSharedStoreIsACache)")
	}
	return url
}

// run names this case's application segment. It is the same inside one case, so
// the two handles a sharing case builds address one keyspace, and different
// between cases, so the entry one case leaves behind is not the entry another
// case expects to find absent — the suite reuses host/example.com in its byte,
// no-lifetime and expiry cases, and on a store one test binary shares with its
// own earlier cases that reuse is a false report about the second one. A shared
// Valkey is what this adapter is for, so the isolation is the key's: every name
// this file writes begins with the application segment, which is the segment
// kit/cache exists to carry.
func run(t *testing.T) string {
	t.Helper()
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	return "pkit-test-" + salt + strconv.FormatUint(uint64(h.Sum32()), 36)
}

// salt keeps two runs of this binary from reading each other's entries: the hash
// above is the same every time, and an entry with a minute left on its clock
// would otherwise be the leftover that makes the next run's case about a miss
// report a hit.
var salt = strings.ReplaceAll(uuid.New().String(), "-", "")[:8]

func connect(t *testing.T, url, app string) cache.Cache {
	t.Helper()
	// Connect, not New: a live server is the thing under test, and a case that
	// began by asking for a PING fails with the server's own sentence rather than
	// with an assertion three commands later.
	c, err := valkey.Connect(t.Context(), config.Cache{App: app, URL: url})
	if err != nil {
		t.Fatalf("valkey.Connect(%s): %v", url, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// lazy is the handle over a store nobody is answering. It has to be New rather
// than Connect for the same reason a deployment uses Connect: nothing here can
// fail on a server nobody is answering, which is what lets the fail-open cases
// reach the commands that have to survive one.
func lazy(t *testing.T, url, app string) cache.Cache {
	t.Helper()
	c, err := valkey.New(config.Cache{App: app, URL: url})
	if err != nil {
		t.Fatalf("valkey.New(%s): %v", url, err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// commands reads one server-side command counter.
func commands(t *testing.T, url, name string) int64 {
	t.Helper()
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL(%s): %v", url, err)
	}
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := client.Info(ctx, "commandstats").Result()
	if err != nil {
		t.Fatalf("INFO commandstats: %v", err)
	}
	for line := range strings.SplitSeq(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "cmdstat_"+name+":") {
			continue
		}
		head, _, found := strings.Cut(strings.TrimPrefix(line, "cmdstat_"+name+":"), ",")
		if !found {
			continue
		}
		calls, err := strconv.ParseInt(strings.TrimPrefix(head, "calls="), 10, 64)
		if err != nil {
			t.Fatalf("INFO commandstats: %s: %v", line, err)
		}
		return calls
	}
	// A command that has never run has no line at all: zero DEL commands have run
	// on a server nobody has deleted anything on.
	return 0
}
