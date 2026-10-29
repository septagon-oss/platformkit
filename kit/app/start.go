package app

import (
	"context"
	"net/http"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func (a *App) prepareRuntime(ctx context.Context) (*httpx.API, http.Handler, cache.Cache, error) {
	// Validate dry registrations before opening deployment resources.
	// pkit may already have requested these declarations.
	if a.declaredRoutes == nil {
		// Declarations classifies refusals before deployment effects.
		if err := a.Declarations(); err != nil {
			return nil, nil, nil, err
		}
	}
	// Build the serving registration over a temporary cache. Transfer to the
	// deployment's cache only after every gate has accepted the same surface.
	segment, err := a.cacheSegment()
	if err != nil {
		a.giveBackDeclared() // no Runtime on this side either
		return nil, nil, nil, beforeEffects(err)
	}
	build := cache.MemoryFor(appname.Name(segment))
	api, handler, err := a.buildAPI(ctx, build)
	if err != nil {
		_ = build.Close() // a composition that failed a gate is never mounted
		// A rejected serving registration must release the constructor’s declaration claim.
		a.giveBackDeclared()
		return nil, nil, nil, beforeEffects(err)
	}
	// Reacquire a released claim before opening resources. Claims are scoped to
	// the application, so another live composition retains its own shapes.
	if err := a.claimDeclared(); err != nil {
		_ = build.Close()
		return nil, nil, nil, beforeEffects(err)
	}
	return api, handler, build, nil
}

func (a *App) connectRuntime(ctx context.Context, api *httpx.API, build cache.Cache) (*db.Conn, cache.Cache, error) {
	// Open deployment resources only after all composition gates pass.
	conn, err := a.openConn(ctx)
	if err != nil {
		a.giveBackDeclared()
		_ = build.Close()
		return nil, nil, err
	}
	store, err := a.cache(ctx)
	if err != nil {
		a.giveBackDeclared()
		_ = conn.Close()
		_ = build.Close()
		return nil, nil, err
	}
	// Connect the gated API before handing out a Runtime or starting a listener.
	// The readiness, rate-limit and denial routes read this same held connection.
	if err := api.Connect(conn, store); err != nil {
		a.giveBackDeclared()
		_ = conn.Close()
		_ = store.Close()
		_ = build.Close()
		return nil, nil, err
	}
	a.held.fill(conn)
	_ = build.Close()
	if err := a.migrate(ctx); err != nil {
		a.giveBackDeclared()
		_ = conn.Close()
		_ = store.Close()
		return nil, nil, err
	}
	return conn, store, nil
}

func (a *App) finishRuntime(ctx context.Context, api *httpx.API, conn *db.Conn, store cache.Cache, handler http.Handler) (*Runtime, error) {
	var err error
	rt := &Runtime{app: a, conn: conn, handler: handler, cache: store,
		declaredRelease: a.takeDeclaredRelease()}
	if a.opts.Role == Worker {
		// Worker routes are validated, but its serving surface contains only probes.
		rt.handler = a.probes(api, conn)
	}
	if a.opts.Role != Web {
		if rt.transport, err = a.transport(); err != nil {
			// Close releases the declaration claim as well as opened resources.
			_ = rt.Close()
			return nil, err
		}
		// Live connections must observe the same transport the outbox relay publishes to.
		a.observed.fill(rt.transport)
	}
	// Install telemetry last: unlike opened resources, replaced process providers
	// cannot be restored by Runtime.Close after a boot failure.
	a.measurement.install(ctx)
	// The completed Runtime owns the resources and declaration release.
	return rt, nil
}
