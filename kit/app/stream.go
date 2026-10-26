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
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
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
//
// The tail mounted here composes to httpx.StreamAddress, the one address a live
// region links: the path is written here and the whole of it there, so a page
// cannot point a connection at an address this route stopped answering.
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
			return nil, problem.New(http.StatusUnauthorized, "AUTH_ANONYMOUS: this connection carries no principal")
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
// — a connection outlives the request that recognised that caller, so the transaction
// that recognised it is long gone.
//
// The questions are asked of the principal this call just recognised, and not of the
// one the connection was opened as. The session behind a live connection is a row that
// other work changes: its roles, and whether it still exists at all. Handing the
// authorizer the context that carries the connect-time principal would ask "what may
// this person do" and answer it with the roles they had when the stream began, which
// is the window a revoked grant should not have.
type streamAuthority struct {
	// conn is the composition's connection, asked for each time it is needed: the
	// one place that knows whether this process holds one yet.
	conn         func(context.Context) (*db.Conn, bool)
	authenticate func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error)
	authorize    httpx.Authorizer
	// catalogue is the composition's permissions as its manifests declared them — the
	// list httpx.Declare receives. The stream asks about a permission token and the
	// authorizer's question is a grant, and the one fact between the two spellings is
	// module.Permission.Operator: an authorizer that reads it must read the flag the
	// manifest wrote and not the zero value beside a token that has none.
	catalogue []tenancy.Grant
}

// grantOf is the catalogue's answer to what kind of permission this token is: false
// for a token no composed module declared, which no ask of this kernel's can name — a
// topic no module streams is refused before anyone is asked.
func (s streamAuthority) grantOf(permission string) tenancy.Grant {
	for _, g := range s.catalogue {
		if g.Permission == permission {
			return tenancy.Grant{Permission: permission, Operator: g.Operator}
		}
	}
	return tenancy.Grant{Permission: permission}
}

func (s streamAuthority) Renew(ctx context.Context, tenant tenancy.Tenant, r *http.Request, asks []string) (stream.Renewal, error) {
	var out stream.Renewal
	conn, ok := s.conn(ctx)
	if !ok {
		return out, errors.New("app: this process holds no connection to re-check a live connection against")
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		principal, stillSignedIn, err := s.authenticate(ctx, tx, r)
		if err != nil {
			return err
		}
		if !stillSignedIn {
			out.SignedIn = false
			return nil
		}
		out.SignedIn = true
		// Every grant is now asked of the caller this transaction just recognised: the
		// authorizer reads the principal on its context, which is the only way it has to
		// know whose roles to resolve.
		ctx = tenancy.WithPrincipal(ctx, principal)
		for _, ask := range asks {
			// The grant asked is the one the catalogue names, operator flag and all, as
			// it is at the route guard. A tenant that is not the operator's is never
			// asked about the operator's own permission at all: kit/stream answers that
			// half from the declaration and the tenant, before this call.
			allowed, err := s.authorize.Allowed(ctx, tenant, s.grantOf(ask))
			if err != nil {
				return err
			}
			if !allowed {
				out.Refused = append(out.Refused, ask)
			}
		}
		return nil
	})
	return out, err
}

// heldTransport is the event transport this composition's live connections watch,
// handed over the way heldConn hands the connection: the door is mounted by New,
// long before anything is opened, and what it is to watch is settled later, by
// Start, because the bus worth watching is the one the composition's own relay
// publishes the outbox onto.
//
// One transport is the point, not one dial saved: the in-process transport
// delivers inside one process, so a transport the door dials for itself is a bus
// nobody ever publishes to. fill hands over the transport Start opened, which that
// Runtime owns and closes; open is for the role with no worker half, which has
// none to be handed and still needs one to watch, and what this holder dials is
// what release gives back.
type heldTransport struct {
	mu       sync.Mutex
	observed events.Transport
	// owned is what open dialled, and so the only thing release may close.
	owned    events.Transport
	released bool
}

// fill is Start's handover, the last thing it does before a Runtime can serve.
func (h *heldTransport) fill(transport events.Transport) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.observed = transport
}

// open answers the question the live door asks: which transport to watch. build is
// transport(), the constructor the worker half is opened with, and it runs at most
// once per composition — a second connection watches what the first was given, and
// a composition that was handed one never calls build. A failed dial is answered to
// the caller waiting for a connection and retried by nobody: streamHub holds that
// answer for the life of the process, as it held the boot's would have been.
func (h *heldTransport) open(build func() (events.Transport, error)) (events.Transport, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return nil, errors.New("app: this composition's Runtime is closed, and a live connection needs one that is open")
	}
	if h.observed != nil {
		return h.observed, nil
	}
	if h.owned == nil {
		transport, err := build()
		if err != nil {
			return nil, err
		}
		h.owned = transport
	}
	return h.owned, nil
}

// release closes what this holder dialled and nothing else: the Runtime closes the
// transport it opened, and the door of another role only ever watches that one.
func (h *heldTransport) release() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.released = true
	owned := h.owned
	h.observed, h.owned = nil, nil
	if closer, ok := owned.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// streamHub builds this composition's hub once. The transport is asked for here
// rather than at the boot because a process that serves no live connection should
// not dial a broker for one — and because the failure, if there is one, is a 500
// that names the broker rather than a boot that died on a listener nobody was
// using. A composition with a worker half has its transport already, opened by
// Start, and watches that one; only a web-role process dials here (heldTransport).
func (a *App) streamHub(registry *stream.Registry) func() (*stream.Hub, error) {
	var once sync.Once
	var built *stream.Hub
	var err error
	return func() (*stream.Hub, error) {
		once.Do(func() {
			transport, transportErr := a.observed.open(a.transport)
			if transportErr != nil {
				err = transportErr
				return
			}
			limits := stream.DefaultLimits()
			limits.MaxPerTenant = a.cfg.Server.Streams.MaxPerTenant
			built = stream.New(stream.Deps{
				Transport: transport,
				Registry:  registry,
				// The grant the door asks with is the grant the route guard is
				// asked about, so the door is handed the list httpx.Declare
				// receives rather than a second reading of the manifests.
				Authority: streamAuthority{conn: a.held.read, authenticate: a.opts.Authenticate, authorize: a.opts.Authorize, catalogue: module.Grants(a.mods)},
				Limits:    limits,
				Log:       a.log,
			})
		})
		return built, err
	}
}
