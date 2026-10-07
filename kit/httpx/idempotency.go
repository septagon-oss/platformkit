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
	// body already has, so a keyed write is bounded exactly as an unkeyed one and
	// this package invents no limit for a caller to trip over.
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

// fingerprint is what the request was, digested. Every field here is one a caller
// can change while believing they retried the same write — the query, the content
// type, one byte of the body — and a record keyed on the body alone would replay a
// write aimed at a different address or of a different shape as the same one.
func fingerprint(method, route, rawQuery, contentType string, body []byte) [sha256.Size]byte {
	h := sha256.New()
	for _, part := range []string{method, route, rawQuery, contentType} {
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
// the ceiling it applied is the ceiling this guard digests under, rather than a
// second bound this package would have to invent and keep in step. It needs the
// caller, whom a.authenticate put on the context and a.authorize has just checked,
// so a replay re-presents the caller's grants and tenant scope before it answers
// anything and a caller who has lost the permission since the first attempt is
// refused by the guard above rather than served a stored answer. It must not sit
// above a.transaction, which may replace the response with a 500 when the commit
// fails: a guard above it would remember an answer the buffer then threw away. And
// the answer it remembers is finalised below it, in respond, which is the one place
// that knows what the client was finally given — so what gets written down is the
// answer, not a draft of one.
func (a *API) idempotency(ctx huma.Context, next func(huma.Context)) {
	store := a.opts.Idempotency
	if store == nil || !keyedSurface(ctx.Context()) || !keyedMethod(ctx.Method()) {
		next(ctx)
		return
	}
	if _, streams := ctx.Operation().Extensions[StreamedBodyExtension]; streams {
		// The one kind of route that reads its own request: its body is a file,
		// digesting it would copy a file, and it answers for its own bytes. It gets
		// no declaration in the document either — see declaresKey.
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
	r.Body = io.NopCloser(bytes.NewReader(body))

	t, _ := tenancy.FromContext(ctx.Context())
	var caller uuid.UUID
	if p, ok := tenancy.PrincipalFrom(ctx.Context()); ok {
		caller = p.UserID
	}
	scope := idempotency.Scope{
		Tenant:      t.ID,
		Caller:      caller,
		Method:      ctx.Method(),
		Route:       routeOf(ctx),
		Key:         key,
		Fingerprint: fingerprint(ctx.Method(), routeOf(ctx), r.URL.RawQuery, r.Header.Get("Content-Type"), body),
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
}

// noteClaim records this attempt on the request's note. It does nothing on a request
// that never reached the mounted API — the case every refusal above the middleware
// shares: nothing was claimed, so there is nothing to land.
func noteClaim(ctx huma.Context, store idempotency.Store, scope idempotency.Scope, claim uuid.UUID) {
	if n, ok := ctx.Context().Value(claimKey{}).(*claimNote); ok {
		n.store, n.scope, n.claim = store, scope, claim
	}
}

// withClaim puts a request's own note in its slot. respond calls it once per
// request, holding the slot its defer reads, and the guard below fills it.
func withClaim(ctx context.Context) context.Context {
	return context.WithValue(ctx, claimKey{}, &claimNote{})
}

// completeBudget bounds the write-down, from kit/limit's budget for the same reason:
// it runs beside an answer already given, and a stalled database must cost the log
// line rather than a response that never finishes.
const completeBudget = 2 * time.Second

// complete writes down the answer this request finally gave.
//
// It runs after the response is final, which is the ordering that keeps a recorded
// 200 from being a committed 500 — the lie buffer.go exists to prevent, pointed the
// other way. A 5xx is deliberately not written down at all: the claim stays open
// until its lease runs out, so a caller whose write died in this process's own
// failure gets a 409 while the kernel cannot say whether the work happened and a
// fresh attempt once it honestly can, rather than a permanent replay of an outage.
//
// It runs on a detached context, so an answer the client received is not
// un-remembered by a client that hung up — and the caller who retries is precisely
// the caller this row is for.
func (a *API) complete(ctx context.Context, n *claimNote, status int, contentType string, body []byte, held bool) {
	if n == nil || n.store == nil || n.claim == uuid.Nil {
		return
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
	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), completeBudget)
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
// operation: the verbs a key is promised on, minus the one kind of route that reads
// its own request (no schema'd body to replay a write of, and no declaration of a
// header it does not honour), minus the control plane.
//
// It is a function of the operation and not of the request, because the document is
// built once at registration and the guard's predicate is the same three facts — a
// route the document says nothing about and a route the guard ignores would be the
// same mistake made twice, in two places that cannot see each other.
func declaresKey(op *huma.Operation) bool {
	if op == nil {
		return false
	}
	switch op.Method {
	case http.MethodPost, http.MethodPatch:
	default:
		return false
	}
	if op.Extensions != nil {
		if _, streams := op.Extensions[StreamedBodyExtension]; streams {
			return false
		}
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
// promise the wire keeps and the promise the document makes cannot drift apart.
const keyDescription = "Replays this write for 24 hours. A retry with byte-identical request and body gets the answer the first attempt was given; the same key with another body is refused 422, and the same key while the first attempt is still running is refused 409. Optional: a write without a key runs as it always did. A UUID is the usual value."

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
