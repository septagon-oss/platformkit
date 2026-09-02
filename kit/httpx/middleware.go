package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// RequestIDHeader is the header a request id arrives in and leaves in.
const RequestIDHeader = "X-Request-ID"

// maxRequestID bounds an id a client supplied. An id is a correlation handle,
// not a payload.
const maxRequestID = 64

type (
	requestIDKey struct{}
	bufferKey    struct{}
	txKey        struct{}
)

// RequestIDFrom returns the id of the request ctx belongs to, or "" outside a
// request. It is the string the caller saw in the response header and in the
// problem body's instance.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// TxFrom returns the request's tenant transaction, opening it if this is the
// first query of the request. It reports false for a request that resolved to
// no tenant, and for one whose transaction could not be opened — the middleware
// logs that failure with its cause, and the handler's own error becomes the
// response.
//
// Opening on demand rather than on arrival is what lets a liveness probe reach
// a tenant host while the database is down: a request that never queries never
// needs a database.
//
// This is how a handler reaches the database: there is no other door, and a
// repository that takes db.Tx[db.Tenant] cannot be called without going through
// one.
func TxFrom(ctx context.Context) (db.Tx[db.Tenant], bool) {
	p, ok := ctx.Value(txKey{}).(*db.Pending)
	if !ok {
		return db.Tx[db.Tenant]{}, false
	}
	tx, err := p.Tx(ctx)
	if err != nil {
		return db.Tx[db.Tenant]{}, false
	}
	return tx, true
}

// requestID gives every request an id: the caller's, when they sent one worth
// keeping, so a trace that starts at a proxy stays one trace; otherwise a fresh
// UUID. It is echoed in the response header, so a caller who sent none can
// still quote it.
func (a *API) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := givenID(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// givenID accepts a client's id only when it is short and printable ASCII. An
// id reaches a log line and a response header, so a newline or a kilobyte in it
// is a forged log entry or a wasted response.
func givenID(s string) string {
	if s == "" || len(s) > maxRequestID {
		return ""
	}
	for _, r := range s {
		if r < '!' || r > '~' {
			return ""
		}
	}
	return s
}

// respond decides what the client finally sees.
//
// It holds the response in a buffer until the handler and its transaction are
// both finished, and it turns a panic into a 500 and a log line with the
// request id, so one bad handler costs one request instead of the process.
//
// The two belong together: a recovery is only able to answer at all because
// nothing has been sent yet. It sits outside the transaction, which is the
// other half of the point — a recovery any deeper would return normally, and a
// transaction that returns normally commits the half-finished work that caused
// the panic. Here the panic unwinds past the transaction middleware, which
// rolls back on its way out, and arrives with an empty buffer.
func (a *API) respond(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := &buffer{ResponseWriter: w, header: w.Header().Clone()}
		r = r.WithContext(context.WithValue(r.Context(), bufferKey{}, b))
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v) // net/http's own signal for "drop this connection quietly"
				}
				a.rlog(r.Context()).ErrorContext(r.Context(), "httpx: handler panicked",
					"method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				if b.reset() {
					writeProblem(b, http.StatusInternalServerError, RequestIDFrom(r.Context()), "")
				}
			}
			b.send()
		}()
		next.ServeHTTP(b, r)
	})
}

// buffer is a response that has not been sent yet.
//
// A 200 written before the commit is a lie whenever the commit can still fail,
// and it can: a DEFERRABLE INITIALLY DEFERRED constraint is checked at COMMIT,
// a serialization failure is raised there, and so is the settings re-read
// kit/db does. Holding the response costs one copy of the body and turns those
// into the 500 they are.
//
// A handler that calls Flush is streaming and knows what it is doing: from that
// call on this is a passthrough, and its response reaches the wire before the
// commit like any other streaming response. A huma StreamResponse that never
// calls Flush is not streaming in any sense the network can see: it is buffered
// whole, like every other response, and it is bounded like every other response.
//
// The bound is maxBuffer. Holding a response costs one copy of it, and a
// download that is larger than that is a response nobody should be copying:
// past the limit the buffer sends what it holds and becomes a passthrough, with
// the same honest consequence as Flush — the bytes are on the wire before the
// commit, and a commit that then fails is a log line rather than a 500.
type buffer struct {
	http.ResponseWriter
	// header is what the response headers were when the buffer was built, so
	// reset can put them back. A Location set by a handler whose transaction
	// then failed to commit must not survive into the 500 that replaces it.
	header http.Header
	status int
	body   bytes.Buffer
	direct bool
}

// maxBuffer is the most a held response may hold. Two megabytes is far past any
// JSON document this API produces and far below anything worth copying.
const maxBuffer = 2 << 20

func (b *buffer) WriteHeader(status int) {
	if b.direct {
		b.ResponseWriter.WriteHeader(status)
		return
	}
	if b.status == 0 {
		b.status = status
	}
}

func (b *buffer) Write(p []byte) (int, error) {
	if !b.direct && b.body.Len()+len(p) > maxBuffer {
		b.send()
	}
	if b.direct {
		return b.ResponseWriter.Write(p)
	}
	return b.body.Write(p)
}

func (b *buffer) Flush() {
	b.send()
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (b *buffer) Unwrap() http.ResponseWriter { return b.ResponseWriter }

// send writes what is held, once.
func (b *buffer) send() {
	if b.direct {
		return
	}
	b.direct = true
	if b.status == 0 {
		b.status = http.StatusOK
	}
	b.ResponseWriter.WriteHeader(b.status)
	if b.body.Len() > 0 {
		_, _ = b.ResponseWriter.Write(b.body.Bytes())
	}
}

// reset discards the held response, headers included, so another can replace
// it. It reports false once the response has begun, which is the case a caller
// cannot take back.
func (b *buffer) reset() bool {
	if b.direct {
		return false
	}
	b.status = 0
	b.body.Reset()
	h := b.ResponseWriter.Header()
	clear(h)
	for k, v := range b.header {
		h[k] = v
	}
	return true
}

// begun reports whether any of the response has reached the wire.
func (b *buffer) begun() bool { return b.direct }

func bufferFrom(ctx context.Context) (*buffer, bool) {
	b, ok := ctx.Value(bufferKey{}).(*buffer)
	return b, ok
}

// writeProblem answers with the one error shape, without huma: the recovery
// runs outside any huma context, and a panic during routing has none at all.
func writeProblem(w http.ResponseWriter, status int, id, detail string) {
	p := problem.New(status, detail)
	if id != "" {
		p.Instance = "urn:request:" + id
	}
	w.Header().Set("Content-Type", problem.ContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// stampRequestID is the response transformer that puts the request id into
// every problem body, as RFC 9457's instance. It lives here rather than in
// kit/problem because kit/problem knows nothing about requests: the shape is
// one package's business, the identifier is another's.
func stampRequestID(ctx huma.Context, _ string, v any) (any, error) {
	if p, ok := v.(*problem.Problem); ok && p.Instance == "" {
		if id := RequestIDFrom(ctx.Context()); id != "" {
			p.Instance = "urn:request:" + id
		}
	}
	return v, nil
}

// rlog is the logger carrying this request's id, so the log line and the
// problem body the caller quotes name the same request.
func (a *API) rlog(ctx context.Context) *slog.Logger {
	if id := RequestIDFrom(ctx); id != "" {
		return a.log.With("request_id", id)
	}
	return a.log
}

// SessionCookie is the cookie a browser session travels in. The name is here,
// and not in the auth module that mints it, because the kernel has to recognise
// it twice without knowing anything else about sessions: to refuse a cross-site
// write (csrf) and to decide that a request is worth authenticating at all.
const SessionCookie = "platformkit_session"

// carry puts the request itself on the context, because the authentication hook
// runs inside the tenant transaction — below huma's routing — and still has to
// read the caller's cookies and headers. It is two lines rather than an adapter
// unwrap so that this package does not care which adapter huma was built on.
func (a *API) carry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestKey{}, r)))
	})
}

// unsafeMethod reports whether a method may change state. The four safe ones are
// the closed set RFC 9110 calls safe; everything else is guarded.
func unsafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return false
	}
	return true
}

// csrf refuses a state-changing request that a cookie session authorised and
// another site sent.
//
// A cookie is attached by the browser whichever page made the request, so a
// session cookie is a credential the caller did not choose to present. The check
// is the browser's own account of where the request came from: Sec-Fetch-Site,
// which a page cannot forge, and an Origin whose host is this one for the older
// clients that do not send it. A request carrying no session cookie is not
// guarded, because nothing was attached on its behalf — a bearer token is
// presented deliberately and a cross-site page cannot read one to present.
func (a *API) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie(SessionCookie); err != nil || !unsafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		switch site := r.Header.Get("Sec-Fetch-Site"); site {
		case "same-origin", "none":
			next.ServeHTTP(w, r)
			return
		case "":
			// No Sec-Fetch-Site at all: an older client, or a non-browser. The
			// Origin header is the fallback, and its absence is accepted for the
			// same reason — a caller that is not a browser attaches no cookie
			// unless somebody wrote the code that does.
			if origin := r.Header.Get("Origin"); origin == "" || sameHost(origin, r.Host) {
				next.ServeHTTP(w, r)
				return
			}
		}
		a.rlog(r.Context()).InfoContext(r.Context(), "httpx: cross-site write refused",
			"method", r.Method, "path", r.URL.Path, "site", r.Header.Get("Sec-Fetch-Site"), "origin", r.Header.Get("Origin"))
		writeProblem(w, http.StatusForbidden, RequestIDFrom(r.Context()),
			"csrf: this request carries a session cookie and came from another site")
	})
}

// sameHost reports whether origin names this request's own host.
func sameHost(origin, host string) bool {
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && HostOnly(u.Host) == HostOnly(host)
}

// requestKey carries the *http.Request past huma's routing. See carry.
type requestKey struct{}

// RequestFrom is the request being served.
//
// A handler wants it for the things huma's typed input cannot express and that
// are properties of the connection rather than of the operation: the host an
// absolute redirect has to be built for, and the address and user agent a
// session records so that a person can recognise it in a list. Reading the body
// through it is a mistake — huma has already decoded it — and reading the
// context off it is another, because this is the request as it was before the
// tenant, the transaction and the principal were put on it.
func RequestFrom(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey{}).(*http.Request)
	return r, ok
}

// authenticate establishes the caller's principal, inside the tenant
// transaction the request has just been given.
//
// It asks the hook only when the request carries something to recognise. That
// is not an optimisation: obtaining the transaction opens it, and a request that
// opens a transaction is a request that fails while the database is down — which
// is exactly what the lazy transaction exists to avoid for a liveness probe
// addressed to a tenant host.
//
// A hook that reports false leaves the context anonymous, which is not an error
// here: only the authorization middleware knows whether the operation minds. A
// hook that returns an error is an outage and answers 500, because a session
// store that cannot be read must not read as "you are not signed in".
func (a *API) authenticate(ctx huma.Context, next func(huma.Context)) {
	r, ok := RequestFrom(ctx.Context())
	if !ok || !credentialed(r) {
		next(ctx)
		return
	}
	tx, ok := TxFrom(ctx.Context())
	if !ok {
		// No tenant, or no transaction. Either way there is nowhere to look the
		// caller up, and the transaction middleware has already logged the cause.
		next(ctx)
		return
	}
	p, ok, err := a.opts.Authenticate(ctx.Context(), tx, r)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not recognise the caller",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "")
		return
	}
	if !ok {
		next(ctx)
		return
	}
	// One recognition, two things put on the context: the principal, which a
	// handler and the authorization middleware ask about, and the actor, which
	// kit/events stamps on every event this request publishes. Deriving the
	// second here rather than at each Publish is what keeps "who did this" out
	// of every module's argument lists.
	next(huma.WithContext(ctx, tenancy.WithActor(WithPrincipal(ctx.Context(), p), p.UserID)))
}

// credentialed reports whether the request presents something the application
// could recognise: the session cookie, or an Authorization header for a caller
// that is not a browser. A request with neither is anonymous without a query.
func credentialed(r *http.Request) bool {
	if _, err := r.Cookie(SessionCookie); err == nil {
		return true
	}
	return r.Header.Get("Authorization") != ""
}

// ConnFrom is the application connection this request is served on.
//
// It is the second half of SystemToken: db.RunSystem takes a connection and a
// capability, and a module holds neither until it is handed them. A module that
// has not taken a token can do nothing with this that it could not already do
// with the request's own transaction.
func ConnFrom(ctx context.Context) (*db.Conn, bool) {
	c, ok := ctx.Value(connKey{}).(*db.Conn)
	return c, ok
}

// WithConn puts a connection on ctx. The transaction middleware calls it for
// every request; it is exported so that a test can put a service in the same
// position a request puts it in, rather than the service growing a second code
// path that exists to be testable.
func WithConn(ctx context.Context, c *db.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, c)
}

type connKey struct{}

// hostTTL is how long a resolved host is believed. Long enough that a busy site
// costs one query per half minute, short enough that adding a domain is a
// coffee rather than a deploy.
const hostTTL = 30 * time.Second

type hostEntry struct {
	tenant tenancy.Tenant
	until  time.Time
}

// hostCache remembers resolutions that succeeded, and only those. Caching a
// failure would turn one blink of the database into thirty seconds of refusals
// for that host, and would let anyone fill the map with Host headers they
// invented; remembering only real tenants bounds it by data the operator owns.
type hostCache struct {
	mu    sync.Mutex
	hosts map[string]hostEntry
}

func (c *hostCache) get(host string) (tenancy.Tenant, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.hosts[host]
	if !ok {
		return tenancy.Tenant{}, false
	}
	if time.Now().After(e.until) {
		// Dropped on the way past, so a host that stopped being served stops
		// occupying the map instead of waiting for a restart.
		delete(c.hosts, host)
		return tenancy.Tenant{}, false
	}
	return e.tenant, true
}

func (c *hostCache) remove(host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.hosts, host)
}

func (c *hostCache) put(host string, t tenancy.Tenant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hosts[host] = hostEntry{tenant: t, until: time.Now().Add(hostTTL)}
}

// resolveTimeout bounds the one query every request makes before it is a
// request. Without it a database that accepts connections and answers nothing
// holds every arriving request open on the client's patience rather than ours.
const resolveTimeout = 2 * time.Second

// InvalidateHost forgets a cached resolution, so a rename or a removal takes
// effect now rather than within hostTTL. The tenant module calls it when it
// changes a host; nothing else has any reason to.
func (a *API) InvalidateHost(host string) { a.hosts.remove(HostOnly(host)) }

// resolve maps a host to a tenant, through the loader, inside a cross-tenant
// transaction the kernel opens for it. The loader is a module: it cannot mint
// the capability itself, and it never holds one outside this call.
//
// Concurrent misses for one host share a single query. A cold cache at the
// front of a traffic spike is otherwise one lookup per request, all of them
// asking the same question.
func (a *API) resolve(ctx context.Context, host string) (tenancy.Tenant, error) {
	if t, ok := a.hosts.get(host); ok {
		return t, nil
	}
	shared, err, _ := a.resolving.Do(host, func() (any, error) {
		if t, ok := a.hosts.get(host); ok {
			return t, nil
		}
		// WithoutCancel, because this lookup is shared: the request that
		// happened to arrive first must not take everyone else's answer with
		// it when it gives up. The timeout is what bounds it instead.
		qctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resolveTimeout)
		defer cancel()
		var t tenancy.Tenant
		err := db.RunSystem(qctx, a.opts.Conn, a.token, func(rctx context.Context, tx db.Tx[db.System]) error {
			var err error
			t, err = a.opts.Tenants.ByHost(rctx, tx, host)
			return err
		})
		return t, err
	})
	if err != nil {
		return tenancy.Tenant{}, err
	}
	t := shared.(tenancy.Tenant)
	if t.ID == uuid.Nil {
		// A loader that answers with the zero Tenant and no error has resolved
		// nothing and does not know it. Taking it at its word would scope the
		// request's transaction to the nil UUID and, worse, make every zero
		// Principal a member of it.
		return tenancy.Tenant{}, fmt.Errorf("the loader returned the zero tenant for %q", host)
	}
	a.hosts.put(host, t)
	return t, nil
}

// tenant resolves the request host to a tenant and puts it on the context,
// where kit/db reads it.
//
// A host that names no tenant is a 404 rather than a 400: from outside, a site
// that is not served and a site that does not exist are the same fact. A loader
// that could not tell is a 503, because an outage that reads as "this site does
// not exist" is how a deployment gets debugged in the wrong direction. The one
// exception is an operation that declared itself Public, which may legitimately
// be reached at a host the loader knows nothing about — a health probe
// addressing the pod by IP, say — and then proceeds with no tenant and, below,
// no transaction.
func (a *API) tenant(ctx huma.Context, next func(huma.Context)) {
	host := HostOnly(ctx.Host())
	t, err := a.resolve(ctx.Context(), host)
	if err == nil {
		next(huma.WithContext(ctx, tenancy.WithTenant(ctx.Context(), t)))
		return
	}
	unknown := errors.Is(err, tenancy.ErrNoSuchHost)
	if unknown {
		a.rlog(ctx.Context()).DebugContext(ctx.Context(), "httpx: no site at host", "host", host)
	} else {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not resolve the host to a tenant",
			"host", host, "error", err)
	}
	if auth, ok := declarationOf(ctx.Operation()); ok && auth.kind == kindPublic {
		next(ctx)
		return
	}
	if unknown {
		_ = huma.WriteErr(a.api, ctx, http.StatusNotFound, "no site is served at "+host)
		return
	}
	ctx.SetHeader("Retry-After", "3")
	_ = huma.WriteErr(a.api, ctx, http.StatusServiceUnavailable, "this host cannot be resolved right now")
}

// HostOnly is the loader's key: the Host header without its port and without
// the brackets an IPv6 literal carries, lower-cased, without the trailing dot a
// fully qualified name may have. Normalising here means every TenantLoader is
// spared doing it, and doing it differently.
//
// It is exported because the module that stores a host has to spell it the same
// way as the middleware that looks one up, and two normalisations that drift is
// a domain that resolves for nobody.
func HostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

// transaction gives the request a transaction it has not opened yet, and ends
// it once the response is decided: commit under a status below 400, roll back
// otherwise, roll back on the way out of a panic.
//
// A request that resolved to no tenant — only a public one reaches here — gets
// none, because db has no tenant to scope one to. A request that resolved to
// one but never queries opens nothing either, which is why a liveness probe
// addressed to a tenant host still answers while the database is down.
func (a *API) transaction(ctx huma.Context, next func(huma.Context)) {
	if _, ok := tenancy.FromContext(ctx.Context()); !ok {
		next(ctx)
		return
	}
	rctx, p, err := db.Lazy(ctx.Context(), a.opts.Conn, a.lazy)
	if err != nil {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: no transaction for this request", "error", err)
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "")
		return
	}
	// Close is idempotent, so this covers the panic path and nothing else.
	defer func() { _ = p.Close(false) }()

	// The connection travels with the transaction: a control-plane route needs
	// both it and a token to open a transaction of its own. See ConnFrom.
	rctx = WithConn(rctx, a.opts.Conn)
	inner := huma.WithContext(ctx, context.WithValue(rctx, txKey{}, p))
	next(inner)

	if openErr := p.Err(); openErr != nil {
		// The handler asked for the transaction and did not get one. Its own
		// error is already the response; this is the cause behind it.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: could not open the request transaction",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", openErr)
	}

	// A status of zero is not a success. huma leaves it at zero on the
	// streaming path, where the handler has already written the answer itself,
	// and it is also what a handler that returned without deciding anything
	// leaves behind. The two are told apart by whether the response has begun:
	// if bytes are on the wire the answer is 200 whatever this middleware
	// thinks, so commit and say so; if nothing has been sent, nobody decided,
	// and a transaction nobody decided about must not commit.
	keep, undecided := statusOf(ctx, a, inner.Status())
	if err := p.Close(keep); err == nil {
		if undecided {
			if b, ok := bufferFrom(ctx.Context()); ok && b.reset() {
				_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "")
			}
		}
		return
	} else if b, ok := bufferFrom(ctx.Context()); ok && b.reset() {
		// Nothing has been sent yet, so the response can still tell the truth.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the request transaction did not commit",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		_ = huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "")
	} else {
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: request transaction failed after the response was written",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
	}
}

// statusOf decides whether the request's transaction commits, and whether the
// response has to be replaced because nothing decided it. See the caller.
func statusOf(ctx huma.Context, a *API, status int) (keep, undecided bool) {
	if status != 0 {
		return status < http.StatusBadRequest, false
	}
	b, ok := bufferFrom(ctx.Context())
	if ok && b.begun() {
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: the response was streamed without a status; committing as 200",
			"method", ctx.Method(), "path", ctx.URL().Path)
		return true, false
	}
	a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the handler decided no status; rolling back",
		"method", ctx.Method(), "path", ctx.URL().Path)
	return false, true
}

// authorize enforces the operation's declaration.
//
// Every refusal is a 403, never a 401: which of the four conditions failed says
// nothing an attacker can act on, but it says a great deal to whoever is
// debugging a deployment, so the code is in the response and the request that
// carried it is in the log.
func (a *API) authorize(ctx huma.Context, next func(huma.Context)) {
	auth, ok := declarationOf(ctx.Operation())
	if !ok {
		// Defense in depth. kit/app runs ValidateDeclarations before it
		// listens, so reaching this branch means an operation was mounted after
		// the gate ran.
		a.deny(ctx, "AUTH_UNDECLARED", "this operation declares no authorization")
		return
	}
	if auth.kind == kindPublic {
		next(ctx)
		return
	}

	p, hasPrincipal := PrincipalFrom(ctx.Context())
	if !hasPrincipal || p.UserID == uuid.Nil {
		a.deny(ctx, "AUTH_ANONYMOUS", "this operation requires a signed-in caller")
		return
	}
	t, hasTenant := tenancy.FromContext(ctx.Context())
	if !hasTenant {
		a.deny(ctx, "AUTH_NO_TENANT", "this operation is tenant work and the host resolved to none")
		return
	}
	// There is no principal-belongs-to-this-tenant check, because there is no
	// way for it to fail: the principal was built from a row read inside this
	// tenant's own transaction. See Principal.
	if auth.kind == kindSignedIn {
		next(ctx)
		return
	}

	grant, _ := auth.grant()
	// The operator check comes before the Authorizer, and that order is the
	// whole point of the declaration. The control plane is served on every
	// tenant's host, so an operator route is reachable at a customer's host by
	// a customer's administrator; asking the roles table first would mean the
	// wildcard that administrator legitimately holds in their own tenant
	// answering a question about everybody's. A tenant that is not the
	// operator's cannot exercise this permission however its roles are
	// written, and there is nothing to ask.
	if grant.Operator && !t.Operator {
		a.deny(ctx, "AUTH_NOT_OPERATOR", grant.Permission+" is the operator's, and this is not the operator's tenant")
		return
	}

	allowed, err := a.opts.Authorize.Allowed(ctx.Context(), t, grant)
	if err != nil {
		// An authorization decision that could not be made is not a denial, and
		// saying so would send a person away from work they are entitled to do.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: authorization decision unavailable",
			"permission", grant.Permission, "operator", grant.Operator, "tenant", t.Slug, "error", err)
		ctx.SetHeader("Retry-After", "3")
		_ = huma.WriteErr(a.api, ctx, http.StatusServiceUnavailable, "authorization is temporarily unavailable")
		return
	}
	if !allowed {
		a.deny(ctx, "AUTH_DENIED", "this operation requires "+grant.Permission)
		return
	}
	next(ctx)
}

// deny logs the machine-readable reason and answers 403 with it. The code is
// the first word of the detail so a log line and a response can be matched
// without adding a field to the one error shape kit/problem defines.
func (a *API) deny(ctx huma.Context, code, detail string) {
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: authorization denied",
		"code", code, "method", ctx.Method(), "path", ctx.URL().Path)
	_ = huma.WriteErr(a.api, ctx, http.StatusForbidden, code+": "+detail)
}
