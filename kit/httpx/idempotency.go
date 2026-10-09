package httpx

// idempotency.go keeps a caller's promise that a write is one write.
//
// The caller says it with `Idempotency-Key` (draft-ietf-httpapi-idempotency-key-header),
// and this is where the kernel does something with it: remember the attempt, hand
// a retry the answer the first attempt got, and refuse the two requests that would
// make the promise false — the same key carrying a different body, and the same key
// while the first attempt is still running.
//
// The memory itself is kit/idempotency, and the reason it is not a map in this
// package is the reason kit/limit's counters are not one either: a retry arrives at
// whichever replica the load balancer chose, minutes later, after a deploy that
// emptied every map in the fleet. What stays here is everything HTTP-shaped about
// the deal — which requests are keyed, what the header may contain, what each
// disposition answers with, and where an answer becomes a remembered one.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// IdempotencyKeyHeader is the caller's promise, spelled once: here, in the
// document that declares it (declaresKey), and in the README.
const IdempotencyKeyHeader = "Idempotency-Key"

const (
	// maxKeyedBody is the largest body a keyed request may carry. It is
	// MaxBodyBytes rather than a second number: that is the ceiling every schema'd
	// body already has, so a keyed write of that shape is bounded exactly as an
	// unkeyed one. It bites on one route only — the one that reads its own request,
	// which takes Options.MaxUpload of it without a key — and that is the deal the
	// header is: a promise about a write costs the memory that keeps the promise, and
	// the alternative to refusing a body this guard cannot digest is running the write
	// twice on the caller's own account. The document says so wherever the header is
	// declared (keyDescription).
	maxKeyedBody = MaxBodyBytes

	// maxRecordedAnswer is the largest answer the memory can hold. Past it the
	// write is remembered as spent rather than replayable, which is the safe
	// direction: losing an answer costs a caller a 409 on its retry, and forgetting
	// a record so a retry can run again costs them the second write the header
	// exists to prevent. A quarter of a megabyte is past every document this API
	// answers with and below the 2 MiB a held response may already reach
	// (buffer.go's maxBuffer), so what it selects for is a streamed or file-shaped
	// answer, which is not a body to keep in a row.
	maxRecordedAnswer = 256 << 10

	// maxKeyLength bounds the header's value. The key is a primary-key column, so
	// an unbounded value is an unbounded row anybody can mint; a hundred twenty-
	// eight characters is far past the UUID the draft recommends.
	maxKeyLength = 128
)

// keyedMethod reports whether a verb is one a key is promised on.
//
// This is deliberately not unsafeMethod. That predicate is "not one of the four
// safe methods", which makes PUT and DELETE keyed writes, and the draft says the
// opposite (draft-ietf-httpapi-idempotency-key-header §1, following RFC 9110):
// OPTIONS, HEAD, GET, PUT and DELETE are already idempotent in the protocol, and
// POST and PATCH are not. A key on a retried PUT buys nothing the verb does not
// already promise, and a record of its body is a body kept for nothing.
func keyedMethod(method string) bool {
	return method == http.MethodPost || method == http.MethodPatch
}

// keyedSurface reports whether this address serves writes a caller may key. The
// control plane is out: the brief names the app and public surfaces, and who may
// replay whose control-plane write is a question nobody has answered yet. One
// constant to widen when they do.
func keyedSurface(ctx context.Context) bool {
	s := SurfaceOf(ctx)
	return s == SurfaceApp || s == SurfacePublic
}

// validKey reports whether a header value is a key, and keyRule is the sentence
// the refusal says when it is not.
//
// The rule is a character class and a length, and nothing else, because the guard
// never parses, re-canonicalises or case-folds a key. Same bytes or a different
// record is what makes the conflicting-body answer decidable: a value normalised
// before it was digested would make two spellings of one UUID one record while the
// caller believes they sent two, and a key that was trimmed or case-folded would
// make somebody's retry a different write.
//
// The accepted set is RFC 9110's token characters — no space, no comma, no quote —
// with exactly one surrounding pair of DQUOTE stripped, because the draft permits a
// Structured-Fields string and a client that sends one sends it quoted.
func validKey(v string) bool {
	inner, quoted := strings.CutSuffix(v, `"`)
	if quoted {
		inner, quoted = strings.CutPrefix(inner, `"`)
	}
	if !quoted && strings.IndexByte(v, '"') >= 0 {
		return false
	}
	if n := len(inner); n == 0 || n > maxKeyLength {
		return false
	}
	return isToken(inner)
}

// keyRule names the rule in the caller's terms: the answer they can act on is a
// UUID, which is what the draft recommends for exactly this reason.
const keyRule = "an idempotency key is 1 to 128 characters of the RFC 9110 token set (a UUID is the usual choice)"

// isToken reports whether every byte is an RFC 9110 token character.
func isToken(v string) bool {
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// addressOf is the path half of the request identity: the address as it arrived on
// the wire, not a decoding of it.
//
// The distinction is not cosmetic. `PATCH /records/a%2Fb/c` and
// `PATCH /records/a/b%2Fc` decode to one path, /records/a/b/c, and route to two
// resources — chi matches on RawPath when the request carries one, and huma then
// unescapes each captured parameter on its own, so the first binds group `a/b` and id
// `c` and the second binds group `a` and id `b/c`. A digest over the decoding cannot
// tell the two writes apart, and the harm of that is the harm of the missing path
// below: one caller's retry answered with the other resource's answer, its own write
// suppressed and nothing on the wire saying so. Digesting the sent bytes is also the
// direction that fails safe — two spellings of one address (`%2F` and `%2f`) are two
// records, which costs a caller a 422 on a retry rather than an answer for a different
// resource. URL.EscapedPath is Go's own verdict on which spelling a request sent:
// it answers RawPath when that is a faithful encoding of Path and the re-escaped Path
// otherwise, so a request that carries no escapes digests the same string it always
// did, and `%20` names one address however the space reached it.
func addressOf(r *http.Request) string { return r.URL.EscapedPath() }

// fingerprint is what the request was, digested. Every field here is one a caller
// can change while believing they retried the same write — the address, the query,
// the content type, one byte of the body — and a record keyed on the body alone
// would replay a write aimed at a different address or of a different shape as the
// same one.
//
// Both the pattern and the path go in, and they answer two different questions.
// The pattern is what a record is *filed* under (Scope.Route, a bounded set of
// strings this package can enumerate); the path is which resource behind that
// pattern the write was aimed at. `PATCH /records/first` and `PATCH /records/second`
// match one pattern and are two writes, and a memory that could not tell them apart
// would answer for a resource it never touched — the second caller told their write
// succeeded while the first one's answer is what came back. That is the reason the
// path enters as addressOf spells it rather than as the router's own decoding.
func fingerprint(method, route, path, rawQuery, contentType string, body []byte) [sha256.Size]byte {
	h := sha256.New()
	for _, part := range []string{method, route, path, rawQuery, contentType} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{'\n'})
	}
	_, _ = h.Write(body)
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum
}

// idempotency remembers the keyed writes of this API's surfaces.
//
// It runs last in the huma chain, after a.bodies, and that place is the point four
// times over. It reads a body, and a.bodies is where reading a body became safe —
// the ceiling it applied is the reason a read here cannot be a request that never
// ends, and the keyed ceiling this guard digests under (maxKeyedBody) is the one the
// document declares, rather than a second bound this package would have to invent and
// keep in step. It needs the caller, whom a.authenticate put on the context and
// a.authorize has just checked, so a replay re-presents the caller's grants and tenant
// scope before it answers anything and a caller who has lost the permission since the
// first attempt is refused by the guard above rather than served a stored answer. It
// must not sit above a.transaction, which may replace the response with a 500 when the
// commit fails: a guard above it would remember an answer the buffer then threw away.
// And the answer it remembers is finalised below it, in respond, which is the one place
// that knows what the client was finally given — so what gets written down is the
// answer, not a draft of one.
func (a *API) idempotency(ctx huma.Context, next func(huma.Context)) {
	store := a.opts.Idempotency
	if store == nil || !keyedSurface(ctx.Context()) || !keyedMethod(ctx.Method()) {
		next(ctx)
		return
	}
	raw := ctx.Header(IdempotencyKeyHeader)
	if raw == "" {
		// The header is optional. A request without it is answered exactly as it
		// was before this middleware existed, and it never touches the store — so a
		// memory that is down costs nothing to the traffic that asked for none.
		next(ctx)
		return
	}
	if !validKey(raw) {
		a.refuse(ctx, http.StatusBadRequest, CodeIdempotencyKey+": "+keyRule)
		return
	}
	key := strings.Trim(raw, `"`)

	r, _ := humachi.Unwrap(ctx)
	// Every keyed request, including the one route that reads its own body. Reading
	// it here is what makes the promise: the digest is the whole of how a retry is
	// known to be the same write, and a guard that stepped around a class of route
	// would be a header that lies on that route — the caller sends the promise, the
	// document declares it, and the write runs twice.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxKeyedBody+1))
	if err != nil {
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: the keyed request body could not be read",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuse(ctx, http.StatusBadRequest, CodeIdempotencyKey+": the request body could not be read")
		return
	}
	if len(body) > maxKeyedBody {
		a.refuse(ctx, http.StatusRequestEntityTooLarge, CodeIdempotencyTooLarge+": a keyed write is remembered up to "+strconv.Itoa(maxKeyedBody)+" bytes")
		return
	}
	// Before next, so the handler decodes the bytes this guard digested rather than
	// an exhausted reader: the replacement reaches huma through the same request it
	// reads a schema'd body from (humachi's BodyReader is this *http.Request's Body),
	// and a re-read of the same reader is what would make the retry a different body.
	//
	// Both requests that may read it are replaced, and they are not always the same
	// one. Every WithContext between here and the handler copies the request, which
	// copies the Body *field*: a reader set on one of them is invisible to the others,
	// and this package keeps two — the one huma decodes a schema'd body from, and the
	// one a.carry put on the context for the route that reads its own (bodies.go is
	// written against that one). A guard that replaced only the first would leave a
	// streamed handler reading the exhausted reader: an empty upload, digested as a
	// write, remembered as one, and never delivered.
	r.Body = io.NopCloser(bytes.NewReader(body))
	if carried, ok := RequestFrom(ctx.Context()); ok && carried != nil && carried != r {
		carried.Body = io.NopCloser(bytes.NewReader(body))
	}

	t, _ := tenancy.FromContext(ctx.Context())
	var caller uuid.UUID
	if p, ok := tenancy.PrincipalFrom(ctx.Context()); ok {
		caller = p.UserID
	}
	scope := idempotency.Scope{
		Tenant: t.ID,
		Caller: caller,
		Method: ctx.Method(),
		Route:  routeOf(ctx),
		Key:    key,
		Fingerprint: fingerprint(ctx.Method(), routeOf(ctx), addressOf(r),
			r.URL.RawQuery, r.Header.Get("Content-Type"), body),
	}

	v, err := store.Claim(context.WithoutCancel(ctx.Context()), scope, time.Now().UTC())
	if err != nil {
		// An outage is never an allowance. A key whose record cannot be read is a
		// promise that cannot be kept, and running the write anyway is the duplicate
		// the caller asked not to make — which is the opposite of how the public
		// write limit fails, and for access.go's stated reason: a limit that cannot
		// be counted lets traffic through, and a memory that cannot be written
		// repeats a write.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the idempotency record could not be read; the write is refused",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuse(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable+": this write could not be remembered, so it was not made")
		return
	}

	switch v.Kind {
	case idempotency.First:
		noteClaim(ctx, store, scope, v.Claim)
		next(ctx)
	case idempotency.Replay:
		a.replay(ctx, v.Stored)
	case idempotency.ConflictBody:
		a.refuse(ctx, http.StatusUnprocessableEntity,
			CodeIdempotencyConflict+": "+key+" was already used for a different write; send this one under a new key")
	case idempotency.InFlight:
		a.refuse(ctx, http.StatusConflict,
			CodeIdempotencyInFlight+": the write keyed "+key+" is still being made; try again in "+
				strconv.Itoa(int(v.Lease.Seconds())+1)+" seconds")
	case idempotency.Spent:
		a.refuse(ctx, http.StatusConflict,
			CodeIdempotencySpent+": the write keyed "+key+" completed and its answer was not kept; read the resource instead")
	default:
		// A verdict this function does not know is a bug in this package, and the
		// caller must not be handed a write on the strength of it.
		a.refuse(ctx, http.StatusInternalServerError, "")
	}
}

// replay answers a retry with the answer its own write was given.
//
// Only Content-Type travels with the stored body. Never Set-Cookie — the public
// surface's promise that nobody standing at it is remembered would be broken by
// one call in a thousand, and respond's writer would withhold it anyway — and never
// Cache-Control or Vary, which are properties of this response rather than of the
// answer. X-Request-ID therefore names this call while a problem document inside
// the body keeps naming the request that did the work: a person quoting a replayed
// 422 is quoting the call that produced it, and the two ids are how an operator
// joins them. That divergence is stated so nobody calls it a bug later.
func (a *API) replay(ctx huma.Context, stored idempotency.Answer) {
	r, w := humachi.Unwrap(ctx)
	if stored.ContentType != "" {
		w.Header().Set("Content-Type", stored.ContentType)
	}
	// Both places, as declared does: the writer holds the answer, and the huma
	// context holds the verdict the transaction middleware commits or rolls back on.
	// A replay runs no handler and writes nothing either way, and committing the
	// transaction that read the record is what keeps it a transaction.
	ctx.SetStatus(stored.Status)
	w.WriteHeader(stored.Status)
	if _, err := w.Write(stored.Body); err != nil {
		a.rlog(r.Context()).WarnContext(r.Context(), "httpx: the replayed answer could not be written",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
	}
}

// claimKey is the slot an in-flight keyed write leaves on the request so respond
// can land the answer it finally gave. See claimNote for why it is a pointer.
type claimKey struct{}

// claimNote is the outward half of the guard: the claim it took, carried beside the
// response so the layer that finalises the answer can record it. It is a pointer in
// a context value for answerNote's reason verbatim — the guard holds a huma.Context,
// the writer holds a *http.Request, and huma's WithContext does not propagate to the
// unwrap the writer reads.
//
// Nothing guards it: the write happens inside the call stack of the read, on one
// goroutine, and the read happens after that stack has returned.
type claimNote struct {
	store idempotency.Store
	scope idempotency.Scope
	claim uuid.UUID
	// ctx is the context the claim was taken on: the one the store's Connections
	// found the pool on, because Claim ran below a.transaction, which is where the
	// connection is put there. The response's own context does not carry it — that
	// middleware is below this slot — so a completion made on the context the defer
	// holds is a lookup that answers ErrNoConnection, which is a write that finished
	// and reports itself in flight forever. Carried beside the claim for that reason,
	// and read through WithoutCancel rather than kept live: the transaction this
	// context may cancel is closed by now, and the pool handle in it is not.
	ctx context.Context
}

// noteClaim records this attempt on the request's note. It does nothing on a request
// that never reached the mounted API — the case every refusal above the middleware
// shares: nothing was claimed, so there is nothing to land.
func noteClaim(ctx huma.Context, store idempotency.Store, scope idempotency.Scope, claim uuid.UUID) {
	if n, ok := ctx.Context().Value(claimKey{}).(*claimNote); ok {
		n.store, n.scope, n.claim, n.ctx = store, scope, claim, ctx.Context()
	}
}

// withClaim puts a request's own note in its slot. respond calls it once per
// request, holding the slot its defer reads, and the guard below fills it.
func withClaim(ctx context.Context) context.Context {
	return context.WithValue(ctx, claimKey{}, &claimNote{})
}

// completeBudget bounds the write-down, from kit/limit's budget for the same reason:
// it runs beside an answer already given, and a stalled database must cost the log
// line rather than a response that never finishes. It is five seconds for the reason
// kit/limit and kit/idempotency give — the shared development cluster measured 1.7s
// and 2.5s for a single statement while its packages were all creating databases at
// once — and here the cost of getting it wrong is worse than a log line: a Complete
// that times out leaves the record claimed and its answer unkept, so the caller who
// retried was refused 409 for a write that had finished. Nothing on the wire waits
// for this, so the bound protects a connection and a worker, not a response.
const completeBudget = 5 * time.Second

// complete writes down the answer this request finally gave.
//
// It runs after the response is final, which is the ordering that keeps a recorded
// 200 from being a committed 500 — the lie buffer.go exists to prevent, pointed the
// other way. Every status the caller was actually given is written down, a 5xx with
// the rest: the caller who retried a 500 gets the same 500 back rather than a second
// attempt at a write whose outcome nobody knows, and the answer they were given is a
// fact this process can still state. What is never written down is an answer that
// was not given — a panic this deep that reset the buffer, or a response that never
// reached the caller — and the claim of a request that ended that way stays open
// until its lease runs out, so the retry is refused 409 while the kernel cannot say
// whether the work happened and allowed once, honestly, it can.
//
// It runs on the claim's own context (§ claimNote.ctx), detached, so neither a client
// that hung up nor the transaction that closed underneath this defer can un-remember
// an answer the caller received — and the caller who retries is precisely the caller
// this row is for.
func (a *API) complete(ctx context.Context, n *claimNote, status int, contentType string, body []byte, held bool) {
	if n == nil || n.store == nil || n.claim == uuid.Nil {
		return
	}
	now := n.ctx
	if now == nil {
		now = ctx
	}
	answer := idempotency.Answer{Status: status, ContentType: contentType, Body: body}
	if !held || len(answer.Body) > maxRecordedAnswer {
		// The bytes were already on the wire, or they are too large to hold: the
		// write is remembered as spent, which costs the caller a 409 and never a
		// second write. One line says which route did it, because a route that
		// always answers larger than this is a route whose keys always spend.
		a.rlog(ctx).WarnContext(ctx, "httpx: the keyed write's answer cannot be remembered whole",
			"method", n.scope.Method, "path", n.scope.Route, "status", status, "bytes", len(body), "held", held)
		answer = idempotency.Answer{}
	}
	detached, cancel := context.WithTimeout(context.WithoutCancel(now), completeBudget)
	defer cancel()
	err := n.store.Complete(detached, n.scope, n.claim, answer, time.Now().UTC())
	if err == nil {
		return
	}
	log := a.rlog(ctx).Warn
	if errors.Is(err, idempotency.ErrLostClaim) {
		// The claim moved on without this answer: the lease ran out and another
		// attempt holds the key. A write happened whose answer will never be shown,
		// which is worth more than a warning.
		log = a.rlog(ctx).Error
	}
	log("httpx: the keyed write's answer could not be remembered",
		"method", n.scope.Method, "path", n.scope.Route, "status", status, "error", err)
}

// declaresKey reports whether the document should declare the header on an
// operation: the verbs a key is promised on, minus the control plane.
//
// It is a function of the operation and not of the request, because the document is
// built once at registration and the guard's predicate is the same two facts — a
// route the document says nothing about and a route the guard ignores would be the
// same mistake made twice, in two places that cannot see each other.
//
// The route that reads its own request is in, and the reason it is not the exception
// it used to be is that the guard bounds what it digests at the same ceiling every
// other keyed write has: a keyed body is at most maxKeyedBody, whatever the route
// would swallow without a key. A route whose answer is larger than maxRecordedAnswer
// spends its key rather than replaying it, which the document says once (§
// keyDescription) instead of leaving a caller to discover on a retry.
func declaresKey(op *huma.Operation) bool {
	if op == nil {
		return false
	}
	switch op.Method {
	case http.MethodPost, http.MethodPatch:
	default:
		return false
	}
	return declaresIdempotencySurface(op)
}

// declaresIdempotencySurface is the surface half of the declaration: the control
// plane's writes are not the writes the brief keys, and the address is the only
// fact about the surface an operation carries at registration. It reads the same
// sentence the request-time chain reads (isOpsPath), so a route cannot be served
// under one rule and documented under another.
func declaresIdempotencySurface(op *huma.Operation) bool {
	return !isOpsPath(op.Path)
}

// keyDescription is what the document says about the header. One string, so the
// promise the wire keeps and the promise the document makes cannot drift apart. The
// byte ceiling is in it because the header is what makes it bite: a route that reads
// its own request takes a body this big without one (Options.MaxUpload), and a caller
// that asks for a promise about a write larger than the promise holds is refused 413
// rather than run unremembered.
var keyDescription = "Replays this write for 24 hours. A retry with byte-identical request and body gets the answer the first attempt was given; the same key with another body, path or query is refused 422, and the same key while the first attempt is still running is refused 409. Optional: a write without a key runs as it always did. A keyed body is bounded to " + strconv.Itoa(maxKeyedBody) + " bytes; a larger write is refused 413 rather than made unremembered. A UUID is the usual value."

// declareKey writes the parameter into the operation the document is built from and
// the middleware reads — the same *huma.Operation, which is the property record
// rests on (recordingAdapter.record). Required is false: the header is optional, and
// a declared-but-optional parameter is how a document says "we will remember this
// one if you ask".
func declareKey(op *huma.Operation) {
	op.Parameters = append(op.Parameters, &huma.Param{
		Name:        IdempotencyKeyHeader,
		In:          "header",
		Description: keyDescription,
		Required:    false,
		Schema:      &huma.Schema{Type: huma.TypeString},
	})
}
