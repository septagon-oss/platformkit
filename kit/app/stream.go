package app

// stream.go is the kernel's own live door: the mount of GET /api/v1/app/stream, and
// the two things the endpoint needs that only the composition can hand it — the
// events transport to observe, and someone to re-ask the guards' questions at every
// heartbeat.
//
// The endpoint itself is kit/stream. What is here is the wiring, and the one decision
// the wiring makes: a stream never holds the request's transaction. It is not the
// stream's own claim but a consequence of what it is — a response that outlives the
// query that admitted it by minutes — so the route declares
// httpx.StreamedResponse() and httpx closes the guard transaction before the handler
// runs, exactly as it does for a route that streams its request. Every transaction a
// live connection opens after that is its own, short, and committed inside the call
// that opened it.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/stream"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// streamInput is the ask: the topics to watch. A repeated parameter and a
// comma-separated list both arrive, because huma hands a []string parameter the raw
// values and splits nothing; kit/stream owns the normalisation, so both spellings
// mean the same thing at this door and in the hub.
//
// There is no tenant here, and none could be: the connection's own resolved tenant is
// the only one a watch may be opened for, and a topic is an event name and not an
// address.
type streamInput struct {
	Topics []string `query:"topics" doc:"Events to watch, each declared by a composed module; repeat the parameter or separate them with commas" example:"task.closed"`
}

// mountStream mounts the kernel's live door beside the catalog route. The address is
// the kernel's because no module owns "the connection every module's declared events
// arrive on"; the guard is httpx.SignedIn(), because which topics a caller may watch
// is decided per topic by the policy each module declared, and one route cannot
// declare N permissions.
func mountStream(api *httpx.API, hub func() (*stream.Hub, error), log *slog.Logger) {
	kernel := api.Surfaces("")
	httpx.Register(kernel.App, huma.Operation{
		OperationID: "app-stream",
		Method:      http.MethodGet,
		Path:        "/stream",
		Summary:     "Watch declared events until the connection ends",
		Description: "Server-Sent Events: one frame per notice, and no payload in any of them. Every topic must be declared by a composed module, and a topic whose policy names a grant this caller does not hold refuses the whole connection. The first frame is always a resync: what the caller knew before this connection is not what this connection can promise.",
		Tags:        []string{"kernel"},
		Errors:      []int{http.StatusBadRequest, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError},
		Extensions:  map[string]any{httpx.StreamedResponseExtension: true},
	}, httpx.SignedIn(), func(ctx context.Context, in *streamInput) (*huma.StreamResponse, error) {
		built, err := hub()
		if err != nil {
			return nil, err
		}
		tenant, ok := tenancy.FromContext(ctx)
		if !ok {
			return nil, problem.New(http.StatusForbidden, "this connection resolves no tenant")
		}
		principal, ok := tenancy.PrincipalFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusForbidden, "AUTH_ANONYMOUS: this connection carries no principal")
		}
		r, ok := httpx.RequestFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusInternalServerError, "this connection carries no request")
		}
		req := stream.Request{Tenant: tenant, Principal: principal, HTTP: r, Topics: in.Topics}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			w, writable := hctx.BodyWriter().(http.ResponseWriter)
			if !writable {
				// Nothing streams over a writer this chain did not hand out, and a
				// stream that began over something else would be a 200 whose frames
				// went nowhere.
				log.ErrorContext(hctx.Context(), "app: the stream's body writer is not a response writer",
					"tenant", tenant.ID, "user", principal.UserID)
				hctx.SetStatus(http.StatusInternalServerError)
				return
			}
			err := built.Serve(hctx.Context(), w, req)
			var refusal *stream.Refusal
			if !errors.As(err, &refusal) {
				if err != nil {
					log.ErrorContext(hctx.Context(), "app: a live connection ended",
						"tenant", tenant.ID, "user", principal.UserID, "error", err)
				}
				// The response is on the wire by now, or nothing was written and the
				// handler said so: the status is handed over so the transaction
				// commits rather than being rolled back for want of a decision.
				hctx.SetStatus(http.StatusOK)
				return
			}
			// A refusal is returned before a byte of the stream is written, so the
			// answer is still this request's to write — and it is a problem document,
			// the same shape every other refusal in this kernel wears.
			hctx.SetStatus(refusal.Status)
			if refusal.RetryAfter > 0 {
				httpx.SetRetryAfter(hctx.Context(), refusal.RetryAfter)
			}
			hctx.SetHeader("Content-Type", problem.ContentType)
			_ = json.NewEncoder(hctx.BodyWriter()).Encode(problem.New(refusal.Status, refusal.Detail))
		}}, nil
	})
}

// streamAuthority asks, inside one short transaction of its own, the two questions
// the request's guards asked once. It is not a second session reader and not a second
// policy engine: it calls the composition's own Options.Authenticate and
// Options.Authorize, and the only thing it adds is the transaction they have to run in
// — a connection outlives the request that opened it, so the transaction that
// recognised that caller is long gone.
type streamAuthority struct {
	// conn is the composition's connection, asked for each time it is needed: the
	// one place that knows whether this process holds one yet.
	conn         func(context.Context) (*db.Conn, bool)
	authenticate func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)
	authorize    httpx.Authorizer
}

func (s streamAuthority) Renew(ctx context.Context, tenant tenancy.Tenant, r *http.Request, grants []string) (stream.Renewal, error) {
	var out stream.Renewal
	conn, ok := s.conn(ctx)
	if !ok {
		return out, errors.New("app: this process holds no connection to re-check a live connection against")
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, stillSignedIn, err := s.authenticate(ctx, tx, r)
		if err != nil {
			return err
		}
		if !stillSignedIn {
			out.SignedIn = false
			return nil
		}
		out.SignedIn = true
		for _, grant := range grants {
			allowed, err := s.authorize.Allowed(ctx, tenant, tenancy.Grant{Permission: grant})
			if err != nil {
				return err
			}
			if !allowed {
				out.Refused = append(out.Refused, grant)
			}
		}
		return nil
	})
	return out, err
}

// streamHub builds this composition's hub once. The transport is opened here rather
// than at the boot because a process that serves no live connection should not dial
// a broker for one — and because the failure, if there is one, is a 500 that names
// the broker rather than a boot that died on a listener nobody was using.
func (a *App) streamHub(registry *stream.Registry) func() (*stream.Hub, error) {
	var once sync.Once
	var built *stream.Hub
	var err error
	return func() (*stream.Hub, error) {
		once.Do(func() {
			transport, transportErr := a.transport()
			if transportErr != nil {
				err = transportErr
				return
			}
			limits := stream.DefaultLimits()
			limits.MaxPerTenant = a.cfg.Server.Streams.MaxPerTenant
			built = stream.New(stream.Deps{
				Transport: transport,
				Registry:  registry,
				Authority: streamAuthority{conn: a.held.read, authenticate: a.opts.Authenticate, authorize: a.opts.Authorize},
				Limits:    limits,
				Log:       a.log,
			})
		})
		return built, err
	}
}
