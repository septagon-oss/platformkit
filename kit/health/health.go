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
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
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

// Func adapts a function to Check, so a check that is one query does not need a
// type of its own.
type Func struct {
	N string
	F func(context.Context) error
}

// Name is the name reported when the check fails.
func (f Func) Name() string { return f.N }

// Check runs the function.
func (f Func) Check(ctx context.Context) error { return f.F(ctx) }

// Register mounts the two probes beside the API, on the router that carries
// neither the request middleware nor a transaction. Both roles therefore answer
// the same bytes from the same handler, which is what the deployment's one
// probe stanza already assumed. See Mux, and httpx.API.Probes.
func Register(api *httpx.API, checks ...Check) {
	api.Probes(Mux(slog.Default(), checks...), livePath, readyPath)
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
func Mux(log *slog.Logger, checks ...Check) http.Handler {
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
			write(w, http.StatusOK, `{"status":"ok"}`, "application/json")
			return
		}
		body, _ := json.Marshal(problem.New(http.StatusServiceUnavailable, "not ready: "+strings.Join(failed, ", ")))
		write(w, http.StatusServiceUnavailable, string(body), problem.ContentType)
	})
	return mux
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
	token := syscap.NewSystemToken("readiness")
	return Func{N: "database", F: func(ctx context.Context) error {
		return db.RunSystem(ctx, conn, token, func(_ context.Context, tx db.Tx[db.System]) error {
			var one int
			return tx.DB().Raw("SELECT 1").Scan(&one).Error
		})
	}}
}
