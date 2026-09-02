// Package health is the two probes an orchestrator calls, and the checks
// behind the second one.
//
// GET /health is liveness: it runs no check of its own, because a probe that
// fails when the database is briefly unreachable gets the process killed instead
// of getting the database fixed. It is still an operation, so a probe sent to a
// host that resolves to a tenant opens that request's transaction like any
// other; an orchestrator addresses the instance directly, which resolves to no
// tenant and therefore touches nothing.
//
// GET /ready is readiness: it runs every registered check and answers 503 with
// the names that failed, so a rolling deploy holds traffic off an instance that
// cannot serve it yet.
package health

import (
	"context"
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
		ctx = probe{ctx}
		var failed []string
		for _, c := range checks {
			if err := c.Check(ctx); err != nil {
				// The name is the answer; the reason is for the operator, and a
				// driver string in a public response is a free map of the
				// deployment.
				slog.ErrorContext(ctx, "health: check failed", "check", c.Name(), "error", err)
				failed = append(failed, c.Name())
			}
		}
		if len(failed) > 0 {
			return nil, problem.New(http.StatusServiceUnavailable, "not ready: "+strings.Join(failed, ", "))
		}
		out := &statusOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}

// probe strips the request's values while keeping its deadline and
// cancellation. A readiness check reports on the process, not on the request
// that happened to trigger it: it must run outside whatever transaction the
// request opened, both because kit/db refuses a system transaction nested in a
// tenant one and because a check answered from inside a request's transaction
// would be answering about that transaction.
type probe struct{ context.Context }

func (probe) Value(any) any { return nil }

// DatabaseCheck reports whether the application connection can reach Postgres,
// with the cheapest statement there is. It runs as a system transaction because
// readiness belongs to no tenant.
func DatabaseCheck(conn *db.Conn) Check {
	token := syscap.NewSystemToken("readiness")
	return Func{N: "database", F: func(ctx context.Context) error {
		return db.RunSystem(ctx, conn, token, func(_ context.Context, tx db.Tx[db.System]) error {
			var one int
			return tx.DB().Raw("SELECT 1").Scan(&one).Error
		})
	}}
}
