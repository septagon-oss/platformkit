// Package health is the two probes an orchestrator calls, and the checks
// behind the second one.
//
// GET /health is liveness: it runs no check of its own, because a probe that
// fails when the database is briefly unreachable gets the process killed instead
// of getting the database fixed. It is still an operation, but it queries
// nothing, and kit/httpx opens a request's transaction only when something asks
// for it — so a liveness probe answers 200 at a tenant host with the database
// down, which is the whole point of a liveness probe.
//
// GET /ready is readiness: it runs every registered check and answers 503 with
// the names that failed, so a rolling deploy holds traffic off an instance that
// cannot serve it yet.
package health

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/problem"
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

type statusOutput struct {
	Body struct {
		Status string `json:"status" doc:"ok when every check passed"`
	}
}

// Register mounts the two probes. Both are Public: an orchestrator has no
// session and reaches the instance at an address that names no tenant.
func Register(api *httpx.API, checks ...Check) {
	httpx.Register(api, huma.Operation{
		OperationID: "health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Liveness",
		Description: "200 while the process is serving. Depends on nothing.",
	}, httpx.Public(), func(context.Context, *struct{}) (*statusOutput, error) {
		out := &statusOutput{}
		out.Body.Status = "ok"
		return out, nil
	})

	httpx.Register(api, huma.Operation{
		OperationID: "ready",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary:     "Readiness",
		Description: "200 when every registered check passes, 503 naming those that did not.",
		Errors:      []int{http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*statusOutput, error) {
		if failed := failures(ctx, slog.Default(), checks); len(failed) > 0 {
			return nil, problem.New(http.StatusServiceUnavailable, "not ready: "+strings.Join(failed, ", "))
		}
		out := &statusOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}

// Mux is the two probes for a process that serves no API: the worker role. It
// is a plain net/http mux, because a worker has no tenant, no session and no
// operation to declare, and building an API to answer two questions would mean
// building the tenancy and authorization middleware to go under it.
//
// It answers exactly what Register answers, in the same shape — kit/problem for
// the failure — because a readiness probe that says one thing in one role and
// another thing in the other is a probe an operator has to learn twice.
func Mux(log *slog.Logger, checks ...Check) http.Handler {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		write(w, http.StatusOK, `{"status":"ok"}`, "application/json")
	})
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
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
