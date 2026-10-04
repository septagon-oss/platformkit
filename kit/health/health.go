// Package health is the two probes an orchestrator calls, and the checks
// behind the second one.
//
// GET /health is liveness: it runs no check of its own, because a probe that
// fails when the database is briefly unreachable gets the process killed
// instead of getting the database fixed.
//
// GET /ready is readiness: it runs every registered check and answers 503 with
// the names that failed, so a rolling deploy holds traffic off an instance that
// cannot serve it yet.
//
// # One mux, both roles
//
// The probes are a plain net/http mux, mounted beside the API in the web role
// and served alone in the worker role. They are not operations: a probe has no
// tenant, no session and nothing to declare, and the API's middleware chain
// resolves a tenant from the request host before any of it runs — a real query,
// with a two second budget, that never hits the cache for a pod address because
// only a successful resolution is cached. Liveness went through that lookup,
// so a probe with a two second timeout failed while the database was
// unreachable and the kubelet restarted every replica during the outage
// instead of after it.
//
// So the two probes bypass the resolver entirely (httpx.API.Probes). Nothing
// else does: a public route is still tenant-scoped, because a page served at a
// customer's host is that customer's page whether or not a session is behind
// it. The exception is exactly the two routes that are addressed to the
// process rather than to a site.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The two paths, spelled once: the mux registers them and the web role mounts
// the mux at them, so a deployment's probe stanza names one string per probe
// and both roles answer at it.
const (
	livePath  = "/health"
	readyPath = "/ready"
)

// Check is one dependency the application needs before it can serve. A module
// contributes its own through its manifest.
type Check interface {
	Name() string
	Check(ctx context.Context) error
}

// Report is one thing an operator reads about the process that is not a verdict:
// the exporter's last success, a queue's depth, a certificate's expiry. It is a
// separate type from Check because the two answer different questions and only one
// of them moves traffic: /ready refuses a request when a Check fails, and a Report
// never does. That difference is why the trace exporter's health is a Report — a
// replica that is serving its tenants perfectly must not be pulled out of the
// rotation because the tracing backend is down — and why a Check-shaped "nice to
// know" is the mistake this type exists to prevent.
//
// The string is the answer, and the error is the reason it is a worrying one, or
// nil. Both reach /ready, and neither decides it.
type Report interface {
	Name() string
	Report(ctx context.Context) (string, error)
}

// There is no Func adapter. One existed to turn a closure into a Check and
// nothing outside this package's own tests ever used it: there is one Check in
// the application, DatabaseCheck below, and a second one arrives with the type
// that needs it.

// Register mounts the two probes beside the API, on the router that carries
// neither the request middleware nor a transaction. Both roles therefore answer
// the same bytes from the same handler, which is what the deployment's one
// probe stanza already assumed. See Mux, and httpx.API.Probes.
func Register(api *httpx.API, checks []Check, reports ...Report) {
	api.Probes(Mux(slog.Default(), checks, reports...), livePath, readyPath)
}

// Mux is the two probes. It is a plain net/http mux because that is all a probe
// needs: no tenant, no session, no operation to declare, and no transaction.
//
// It is the whole answer in both roles — the worker serves it alone, the web
// role mounts it beside the API — because a readiness probe that says one thing
// in one role and another thing in the other is a probe an operator has to
// learn twice. It was two implementations of two routes until the web role's
// probes had to stop resolving a tenant, at which point the one that already
// did not was the answer.
func Mux(log *slog.Logger, checks []Check, reports ...Report) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+livePath, func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, `{"status":"ok"}`, "application/json")
	})
	mux.HandleFunc("GET "+readyPath, func(w http.ResponseWriter, r *http.Request) {
		failed := failures(r.Context(), log, checks)
		if len(failed) == 0 {
			write(w, http.StatusOK, ready(r.Context(), reports), "application/json")
			return
		}
		body, _ := json.Marshal(problem.New(http.StatusServiceUnavailable, "not ready: "+strings.Join(failed, ", ")))
		write(w, http.StatusServiceUnavailable, string(body), problem.ContentType)
	})
	return mux
}

// ready is readiness's body: the same two bytes it has always been when there is
// nothing to say, and the reports appended when a composition registers them. The
// verdict is unchanged either way — a Report cannot move it — so an existing probe
// stanza reads the same answer as before. Keys are sorted by encoding/json, so the
// bytes do not depend on the order two reports were registered in.
//
// The reports run on the probe request's context, the same one failures is handed:
// a Report that asks a database or an upstream is bounded by the client that asked,
// so a probe that hangs up or times out stops the work instead of leaving a
// goroutine per poll running past the request. A Report run on context.Background()
// could not be cancelled by anything.
func ready(ctx context.Context, reports []Report) string {
	if len(reports) == 0 {
		return `{"status":"ok"}`
	}
	seen := map[string]string{}
	for _, r := range reports {
		msg, err := r.Report(ctx)
		if err != nil {
			msg = msg + ": " + err.Error()
		}
		seen[r.Name()] = msg
	}
	body, err := json.Marshal(struct {
		Status  string            `json:"status"`
		Reports map[string]string `json:"reports"`
	}{"ok", seen})
	if err != nil {
		// A map of strings cannot fail to marshal, so this line is unreachable and
		// the answer it gives is the one that was true before reports existed.
		return `{"status":"ok"}`
	}
	return string(body)
}

func write(w http.ResponseWriter, status int, body, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// failures runs every check and names the ones that did not pass. The name is
// the answer; the reason is for the operator, and a driver string in a public
// response is a free map of the deployment.
func failures(ctx context.Context, log *slog.Logger, checks []Check) []string {
	var failed []string
	for _, c := range checks {
		if err := c.Check(ctx); err != nil {
			log.ErrorContext(ctx, "health: check failed", "check", c.Name(), "error", err)
			failed = append(failed, c.Name())
		}
	}
	return failed
}

// DatabaseCheck reports whether the application connection can reach Postgres,
// with the cheapest statement there is. It runs as a system transaction because
// readiness belongs to no tenant; the probe request has opened none of its own,
// so there is no tenant transaction for this one to be nested in.
func DatabaseCheck(conn *db.Conn) Check {
	return DatabaseCheckThrough(func(context.Context) (*db.Conn, bool) { return conn, conn != nil })
}

// DatabaseCheckThrough is DatabaseCheck for a composition that built its routes
// before it opened its connection: the probe asks its source when a probe arrives
// rather than when the router was built, which is the difference between a readiness
// route that answers "no" while the pool is still closed and one that dereferences
// a connection nobody handed it. kit/app builds its API — this check included —
// before it dials anything, because the routes are the last thing a composition can
// get wrong for free.
func DatabaseCheckThrough(source func(context.Context) (*db.Conn, bool)) Check {
	return database{source: source, token: syscap.NewSystemToken("readiness")}
}

// database is the one Check this application has.
type database struct {
	source func(context.Context) (*db.Conn, bool)
	token  tenancy.SystemToken
}

func (database) Name() string { return "database" }

func (d database) Check(ctx context.Context) error {
	conn, ok := d.source(ctx)
	if !ok {
		return errors.New("no connection has been opened for this process yet")
	}
	return db.RunSystem(ctx, conn, d.token, func(_ context.Context, tx db.Tx[db.System]) error {
		var one int
		return tx.DB().Raw("SELECT 1").Scan(&one).Error
	})
}
