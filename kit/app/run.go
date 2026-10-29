package app

import (
	"context"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// flushTelemetry exports buffered measurements at most once per App, after
// runtime resources are released. An uninstalled plan does nothing; flush
// failures are logged without turning shutdown into an error.
func (a *App) flushTelemetry(ctx context.Context) {
	a.telemetryOnce.Do(func() {
		if err := a.measurement.flush(ctx); err != nil {
			a.log.ErrorContext(ctx, "app: telemetry was not flushed", "error", err)
		}
	})
}

// shutdownGrace bounds the wait for in-flight requests once the context is done.
const shutdownGrace = 10 * time.Second

// Run answers every gate, migrates, then serves or works or both, and returns
// when ctx is done. It is
// Start, whichever halves this process's role names, and Close; a caller that owns
// its own listener uses those parts directly instead. See lifecycle.go.
func (a *App) Run(ctx context.Context) error {
	// Registered first so it runs last — after the listener and the work have
	// stopped and after Close has returned the connection. A whole boot flushes
	// through Close; this defer is the case that never got there, and since the
	// install moved to the end of Start it is a quiet one: a composition that failed
	// a gate or an open installed nothing, so it has no spans of its own to get out.
	// It stays because Run is the door a process ends through, and because the Once
	// inside flushTelemetry makes arriving twice and arriving once one call. It gets
	// the same grace the requests got, on a context the cancelled shutdown cannot cut
	// short.
	defer func() {
		grace, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		a.flushTelemetry(grace)
	}()
	rt, err := a.Start(ctx)
	if err != nil {
		return err
	}
	// Start opened the transport the role names, so nothing listens until a broker
	// that is down has already refused this boot; Close releases both halves.
	defer rt.Close()

	if a.opts.Role == Web {
		return a.serve(ctx, rt.handler)
	}
	if a.opts.Role == Worker {
		return a.work(ctx, rt.conn, rt.transport, rt.handler)
	}
	// One process, both halves. The web half owns the listener, so the worker
	// half is given no handler of its own.
	return a.both(ctx, rt.conn, rt.transport, rt.handler)
}

// probes is the worker's whole HTTP surface: liveness and readiness, on the
// same address the web role listens on, so one orchestrator manifest describes
// both roles. kit/health owns the shape, so the two roles answer alike — which
// includes the shape of a refused probe: the API this composition already built
// is handed over rather than a worker that answers an outage with a body of its
// own. The worker role builds the API on the way through Start whatever role it
// serves, so this costs no second one.
func (a *App) probes(api *httpx.API, conn *db.Conn) http.Handler {
	return health.MuxWithFaults(a.log, api.Siteless, []health.Check{health.DatabaseCheck(conn)}, a.reports...)
}
