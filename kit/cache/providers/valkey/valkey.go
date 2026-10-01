// Package valkey is the shared store behind kit/cache's port.
//
// One process every replica can reach is the whole requirement. A value that a
// suspension cannot reach is not a cache — it is a stale answer with an owner who
// cannot find it, which is the failure this kernel exists to close. This is the
// one adapter kit/cache ships: the four commands its Backend port asks for and
// nothing else, because everything the invalidation depends on — the generation
// an entry is written under, the envelope that carries it, the budget on every
// call, the refusal of an entry with no lifetime — lives in kit/cache and is
// therefore not re-derivable wrongly here. The adapter runs the same conformance
// suite as the in-process store (cachetest.Conformance) for exactly that reason.
//
// The commands are MGET, SET with an expiry, DEL of exact keys and INCR of one
// counter. No KEYS, no SCAN, no prefix delete, no FLUSHDB, no pub/sub and no
// client-side caching: a scan's cost grows with everything the installation has
// ever stored, and an invalidation that works by matching a prefix is an
// invalidation that can match somebody else's key. A Move here is one INCR of a
// key named in full by kit/cache.
package valkey

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
)

// deleteChunk bounds one DEL command. The host invalidation a suspension triggers
// carries every host of one tenant (modules/tenant calls InvalidateHost with the
// whole set), so a batch is the normal case rather than the large one; one
// command per key would be a request whose round trips grow with the tenant's
// host list, and one command for all of them would be a command whose argument
// list nobody bounded. 500 is a round trip with 500 arguments, which is the
// shape a server answers comfortably.
const deleteChunk = 500

// connectBudget bounds the boot probe. It is deliberately not kit/cache's 250 ms
// request budget: that number bounds one lookup inside a request that is already
// moving, and a cold DNS resolution on the first call after a deploy is routinely
// slower than it. A boot that refused on that would turn a slow first request into
// no server at all, which is the worse of the two. Two seconds is the wait an
// operator watching a crash loop notices, and it is still a refusal rather than a
// hang.
const connectBudget = 2 * time.Second

// New returns the port over a store this process has not yet spoken to.
//
// go-redis opens connections on the first command, so nothing here can fail for a
// server that is down — which is what makes New the right constructor for a test
// double pointed at an address with nothing behind it, and the wrong one for a
// boot. A deployment that says its cache is shared wants the failure at boot:
// that is what Connect is, and kit/app calls it.
func New(cfg config.Cache) (cache.Cache, error) {
	opts, err := parseURL(cfg)
	if err != nil {
		return nil, err
	}
	return cache.New(cfg.App, &backend{client: redis.NewClient(opts)})
}

// Connect is New plus the boot probe: one PING inside connectBudget, so a
// composition that named a shared store and cannot reach it refuses to start
// rather than serving private answers from a cache nobody can invalidate.
func Connect(ctx context.Context, cfg config.Cache) (cache.Cache, error) {
	opts, err := parseURL(cfg)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	probe, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectBudget)
	defer cancel()
	if err := client.Ping(probe).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("cache: the store at %s did not answer within %s: %w", describe(cfg.URL), connectBudget, scrub(cfg.URL, err))
	}
	return cache.New(cfg.App, &backend{client: client})
}

// parseURL is the one place an address becomes a client configuration — and, for
// the reason describe and scrub give, the one place it is refused without being
// repeated in full.
func parseURL(cfg config.Cache) (*redis.Options, error) {
	raw := cfg.URL
	// valkey:// is the scheme an operator reaches for when naming a Valkey, and
	// the server itself answers RESP and a redis_version field. go-redis accepts
	// only its own two schemes, so the rewrite happens here, once, and the
	// configuration may keep saying what the thing is called.
	if u, err := url.Parse(raw); err == nil && u.Scheme == "valkey" {
		u.Scheme = "redis"
		raw = u.String()
	}
	parsed, err := redis.ParseURL(raw)
	if err != nil {
		// net/url's own failure repeats the address it was handed, userinfo and all,
		// and this package cannot pick a credential out of a string it could not read:
		// the refusal of an unparsable address names the setting and nothing else.
		// Every other parse failure comes from an address this package did read, so it
		// is quoted after scrub, which leaves the host and the part that is wrong.
		if _, perr := url.Parse(raw); perr != nil {
			return nil, errors.New("cache: cache.url is not an address this package can read; it must be redis://, rediss://, valkey:// or unix://")
		}
		return nil, fmt.Errorf("cache: cache.url %s: %w", describe(raw), scrub(raw, err))
	}
	if cfg.Password != "" {
		parsed.Password = cfg.Password
	}
	return parsed, nil
}

// describe is an address as this package may write it into a sentence: scheme,
// host, port and path — and no userinfo, no query and no fragment.
//
// kit/config refuses credentials in cache.url, but New and Connect are exported
// constructors a composition may call with a config.Cache it assembled itself, and
// the form go-redis itself documents for a password is the userinfo of
// redis://:secret@host. An address about which nothing can be established is not
// echoed at all: the setting's name is the actionable half of that refusal.
func describe(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Host == "" && u.Path == "") {
		return "(an address this package cannot read)"
	}
	u.User, u.RawQuery, u.RawPath, u.Fragment, u.RawFragment = nil, "", "", "", ""
	return u.String()
}

// secrets is every credential the address itself carries: the userinfo password and
// any password-shaped query value. config.Cache.Password never reaches a sentence
// this package writes, so it needs no taking out.
func secrets(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	var out []string
	if pass, has := u.User.Password(); has {
		out = append(out, pass)
	}
	for _, key := range []string{"password", "pass", "pwd"} {
		if v := u.Query().Get(key); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// scrub takes those credentials out of an error assembled by the client library,
// which builds some of its messages out of the address it was handed: net/url
// repeats the whole URL, and go-redis names an option it did not recognise in full.
// That is how a store's password reaches a boot log, and then a log shipper.
//
// It is a no-op for an address carrying nothing, which is every address kit/config
// accepted, so the wrapping stays and errors.Is still reaches the server's refusal.
func scrub(raw string, err error) error {
	list := secrets(raw)
	if len(list) == 0 {
		return err
	}
	msg := err.Error()
	for _, secret := range list {
		if secret != "" {
			msg = strings.ReplaceAll(msg, secret, "<redacted>")
		}
	}
	return &scrubbed{msg: msg, err: err}
}

// scrubbed is a sentence with no credential in it and its cause still attached, so
// scrubbing costs a caller nothing: errors.Is and errors.As reach what it reworded.
type scrubbed struct {
	msg string
	err error
}

func (s *scrubbed) Error() string { return s.msg }
func (s *scrubbed) Unwrap() error { return s.err }

// backend is the four commands. It holds no rule about what the bytes mean: it
// has never seen a generation, and it does not need to.
type backend struct {
	client *redis.Client

	mu     sync.Mutex
	closed bool
}

// live refuses every command once Close has run, with the error the port promises
// for it. A command that reached a closed client would answer with go-redis's own
// "client is closed" — true, and not the sentence this port documented.
func (b *backend) live() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return cache.ErrClosed
	}
	return nil
}

func (b *backend) GetMany(ctx context.Context, keys ...string) ([]cache.Value, error) {
	if err := b.live(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, nil
	}
	cmd := b.client.MGet(ctx, keys...)
	if err := cmd.Err(); err != nil {
		return nil, err
	}
	replied := cmd.Val()
	if len(replied) != len(keys) {
		// A short answer would be read against the wrong key, which is how a
		// tenant's entry becomes another tenant's. Refusing is cheaper than that.
		return nil, fmt.Errorf("valkey: MGET answered %d values for %d keys", len(replied), len(keys))
	}
	values := make([]cache.Value, 0, len(replied))
	for i, v := range replied {
		switch val := v.(type) {
		case nil:
			values = append(values, cache.Value{})
		case []byte:
			values = append(values, cache.Value{Val: val, Found: true})
		case string:
			values = append(values, cache.Value{Val: []byte(val), Found: true})
		default:
			return nil, fmt.Errorf("valkey: key %d holds a %T; this store holds bytes", i, v)
		}
	}
	return values, nil
}

func (b *backend) Set(ctx context.Context, key string, val []byte, ttl time.Duration) error {
	if err := b.live(); err != nil {
		return err
	}
	// The lifetime is always positive: kit/cache refuses an entry with none before
	// it gets here, because this command's own default for a missing expiry is
	// "forever". go-redis sends PX when the duration is under a second, so a short
	// entry costs its own precision rather than the round up to a second.
	return b.client.Set(ctx, key, val, ttl).Err()
}

func (b *backend) Delete(ctx context.Context, keys ...string) error {
	if err := b.live(); err != nil {
		return err
	}
	for start := 0; start < len(keys); start += deleteChunk {
		end := min(start+deleteChunk, len(keys))
		if err := b.client.Del(ctx, keys[start:end]...).Err(); err != nil {
			return err
		}
	}
	return nil
}

func (b *backend) Raise(ctx context.Context, key string) (int64, error) {
	if err := b.live(); err != nil {
		return 0, err
	}
	// INCR is the only command here that mutates a counter, and it is atomic on
	// the server, so two moves of one namespace cannot lose one another. The
	// counter is never given a TTL: a generation that expired would reopen every
	// entry written under a closed one (kit/cache:generationKey).
	return b.client.Incr(ctx, key).Result()
}

func (b *backend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return cache.ErrClosed
	}
	b.closed = true
	if err := b.client.Close(); err != nil {
		return fmt.Errorf("valkey: %w", err)
	}
	return nil
}
