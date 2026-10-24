package app

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// heldConn supplies the connection to routes registered before the pool opens.
// Start fills it after all route gates pass; readers never open a connection.
type heldConn struct {
	mu   sync.RWMutex
	conn *db.Conn
}

// fill is the one write: Start's, once, between the last gate and the first
// listener, and beside the httpx.API.Connect that hands the same connection to the
// router.
func (h *heldConn) fill(conn *db.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conn = conn
}

// read is the one question, and the shape kit/limit asks its connection source in.
func (h *heldConn) read(context.Context) (*db.Conn, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.conn, h.conn != nil
}

// openConn opens the application connection, as the role row-level security
// binds.
func (a *App) openConn(ctx context.Context) (*db.Conn, error) {
	return db.OpenWithPool(ctx, a.cfg.Database.URL, databasePool(a.cfg.Database))
}

// transport is the event transport this role uses: the injected one, or the one
// the application's constructor for the selected mode builds. New has already
// refused a missing constructor, so the errors here are the constructor's own — a
// broker that cannot be reached, or one that answered with nothing at all. The two
// refusals below name the option the application owns, in the same words
// Transports.constructor uses for a constructor that is missing.
func (a *App) transport() (events.Transport, error) {
	if a.opts.Transport != nil {
		return a.opts.Transport, nil
	}
	broker, err := useJetStream(a.cfg.NATS.Transport, a.opts.Role)
	if err != nil {
		return nil, err
	}
	build, err := a.opts.Transports.constructor(broker)
	if err != nil {
		return nil, err
	}
	transport, err := build(a.cfg.NATS)
	if err != nil {
		return nil, err
	}
	// A constructor that returns (nil, nil) has not supplied a transport, it has
	// reported that it did. Nothing downstream recovers from that: the worker half
	// would be a scheduler handing outbox rows to a nil interface, and the caller of
	// Start would hold a Runtime whose work half cannot work. So it is refused at the
	// boot, where the connection comes back, rather than at the first tick.
	if transport == nil {
		if broker {
			return nil, errors.New("app: nats.transport selects jetstream and Options.Transports.JetStream answered with no transport and no error")
		}
		return nil, errors.New("app: nats.transport selects memory and Options.Transports.Memory answered with no transport and no error")
	}
	return transport, nil
}

// defaultCacheApp is the first segment of every key this kernel writes when the
// configuration names no application: the reference composition's own. It is a
// default and not a secret — an installation with more than one product behind one
// store is exactly the installation that must set cache.app, and cache.Validate
// refuses to leave it empty once a shared store is named.
const defaultCacheApp = "pkit"

// cacheSegment is the first segment of every key this kernel writes: cache.app,
// or the reference composition's own name when the installation named none. It is
// checked here, before any store is built, because the one error these
// constructors answer for a name is this one, and a boot that panics on its own
// configuration is a worse log than a boot that names the key to fix — and
// cache.MemoryFor answers that name with a panic, which is the reason Declarations
// asks for the segment the same way rather than handing a store straight to
// httpx.
func (a *App) cacheSegment() (string, error) {
	segment := a.cfg.Cache.App
	if segment == "" {
		segment = defaultCacheApp
	}
	if _, err := cache.Slug(segment); err != nil {
		return "", fmt.Errorf("app: cache.app: %w", err)
	}
	return segment, nil
}

// cache is the store this process reads values from: the shared one the
// configuration names, or the in-process one when it names none. Where a shared
// value lives is the runner's choice, which is why it is made here beside
// transport() and not inside the package that reads a value.
func (a *App) cache(ctx context.Context) (cache.Cache, error) {
	segment, err := a.cacheSegment()
	if err != nil {
		return nil, err
	}
	build, err := a.opts.Caches.constructor(a.cfg.Cache.Adapter)
	if err != nil {
		return nil, err
	}
	if build == nil {
		a.log.InfoContext(ctx, "cache: in-process store; forgetting a value reaches this process only")
		return cache.MemoryFor(appname.Name(segment)), nil
	}
	store, err := build(ctx, a.cfg.Cache)
	if err != nil {
		return nil, fmt.Errorf("app: cache.adapter %s: %w", a.cfg.Cache.Adapter, err)
	}
	// The same refusal transport() makes of a constructor: answering (nil, nil) is
	// reporting success, and a request that then reads a nil store is a panic in a
	// middleware rather than a boot that came back.
	if store == nil {
		return nil, fmt.Errorf("app: cache.adapter %s answered with no store and no error", a.cfg.Cache.Adapter)
	}
	return store, nil
}

func useJetStream(mode string, role Role) (bool, error) {
	switch mode {
	case "":
		return role != All, nil
	case "jetstream":
		return true, nil
	case "memory":
		if role == All {
			return false, nil
		}
		return false, errors.New("app: nats.transport memory requires role all; separate workers need a shared broker")
	default:
		return false, errors.New("app: nats.transport must be memory, jetstream or empty for the role default")
	}
}
