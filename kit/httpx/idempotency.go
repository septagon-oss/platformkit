package httpx

// Idempotency-Key binds a POST/PATCH body to one result. kit/idempotency stores
// claims across replicas; this adapter owns HTTP admission, replay and finalization.
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

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/operation"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// IdempotencyKeyHeader is the caller's promise, spelled once: here, in the
// document that declares it (declaresKey), and in the README.
const IdempotencyKeyHeader = "Idempotency-Key"

const (
	// Keyed uploads use the ordinary body ceiling so the complete body can be
	// hashed before admission, even on a route that otherwise streams uploads.
	maxKeyedBody = MaxBodyBytes

	// Larger answers leave a spent record: losing replay bytes must not let a
	// retry execute the write again. Streaming/file answers need no stored body.
	maxRecordedAnswer = 256 << 10

	// maxKeyLength bounds the header's value. The key is a primary-key column, so
	// an unbounded value is an unbounded row anybody can mint; a hundred twenty-
	// eight characters is far past the UUID the draft recommends.
	maxKeyLength = operation.MaxTokenLength
)

// keyedMethod selects POST/PATCH; PUT/DELETE already promise HTTP idempotence
// without retaining a keyed copy of their bodies.
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
	return request.ValidToken(inner)
}

// keyRule names the rule in the caller's terms: the answer they can act on is a
// UUID, which is what the draft recommends for exactly this reason.
const keyRule = "an idempotency key is 1 to 128 characters of the RFC 9110 token set (a UUID is the usual choice)"

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
	if a.opts.Operations != nil {
		a.operationAdmission(ctx, next)
		return
	}
	store := a.opts.Idempotency
	if store == nil || !keyedSurface(ctx.Context()) || !keyedMethod(ctx.Method()) || mintsACredential(ctx.Operation()) {
		// The same three facts the document reads, read here: a route the document
		// says nothing about and a route the guard ignores are one mistake made twice,
		// and the third of them — a route whose answer is a credential it minted —
		// cannot be answered from a memory at all. See CredentialExtension.
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
		// A key-only command acts on the key, so its holder's other keys are
		// different callers even when their request bytes are identical.
		if auth, _ := declarationOf(ctx.Operation()); auth.kind == kindBearerKey {
			caller = p.TokenID
		}
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
		// Before the handler, on the store, and before the note: this is the mark that
		// says an attempt got as far as the write, and a write it committed may still
		// be there when its answer is nowhere to be found. Put it on first, because
		// the fact has to be durable before the thing it records can happen, and put
		// it on failing closed, because a record that cannot be marked is a promise
		// this attempt cannot keep and the write has to wait for it.
		//
		// An attempt that never gets past this line leaves no mark, which is what
		// keeps the abandoned claim takeable: the store being down is never a reason a
		// caller can never retry a write that demonstrably never ran.
		if err := store.Begin(context.WithoutCancel(ctx.Context()), scope, v.Claim, time.Now().UTC()); err != nil {
			a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the keyed write could not be marked as begun; the write is refused",
				"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
			a.refuse(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable+": this write could not be remembered, so it was not made")
			return
		}
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

// replay rechecks current authority and representation before sending stored bytes.
// Content-Type and keptHeaders travel with the answer; cookies never do. Cache/Vary
// and X-Request-ID belong to this exchange, while a stored problem retains its
// original request identity. Recorded limits retain their decision and age their wait.
func (a *API) replay(ctx huma.Context, stored idempotency.Answer) {
	if auth, ok := declarationOf(ctx.Operation()); ok && auth.replay != nil {
		tx, present := TxFrom(ctx.Context())
		if !present {
			a.operationUnavailable(ctx, db.ErrNoTenant)
			return
		}
		allowed, err := (*auth.replay)(ctx.Context(), tx)
		if err != nil {
			a.operationUnavailable(ctx, err)
			return
		}
		if !allowed {
			a.deny(ctx, CodeDenied, "current authority does not permit recovering this answer")
			return
		}
	}
	if !a.replayRepresentation(ctx, stored.ContentType, stored.Status) {
		return
	}
	r, w := humachi.Unwrap(ctx)
	if stored.ContentType != "" {
		w.Header().Set("Content-Type", stored.ContentType)
	}
	// Replace current guard headers with the recorded decision, including redirects.
	for _, h := range stored.Headers {
		w.Header().Set(h.Name, h.Value)
	}
	// Restate the original ceiling with the remaining wait, not this retry's counter.
	sayRecorded(w.Header(), stored.Limits, stored.Status == http.StatusTooManyRequests, time.Now().UTC())
	// The writer sends the answer; Huma's status also governs transaction completion.
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
	store      idempotency.Store
	scope      idempotency.Scope
	claim      uuid.UUID
	operation  bool
	noReplay   bool
	failureErr error
	// ctx is the context the claim was taken on: the one the store's Connections
	// found the pool on, because Claim ran below a.transaction, which is where the
	// connection is put there. The response's own context does not carry it — that
	// middleware is below this slot — so a completion made on the context the defer
	// holds is a lookup that answers ErrNoConnection, which is a write that finished
	// and reports itself in flight forever. Carried beside the claim for that reason,
	// and read through WithoutCancel rather than kept live: the transaction this
	// context may cancel is closed by now, and the pool handle in it is not.
	ctx context.Context
	// uncommitted says this attempt's transaction ended without a COMMIT this process
	// could read the answer to. That covers the two failures the transaction middleware
	// can report and the record cannot resolve on its own: the commit was refused, so
	// nothing was written, and the commit was sent and its acknowledgement was lost, so
	// the write may or may not be there. Neither is an answer worth remembering, because
	// the answer the caller was given — a 500, or a 200 that never reached anybody —
	// describes an outcome the kernel does not know. The attempt keeps its key and
	// settles no verdict; the effect marker resolves it on the next press (Store.Mark).
	uncommitted bool
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

// noteUncommitted marks that the attempt holding this request's claim may not record
// an answer. It does nothing on a request that claimed no key, which is every request
// the guard stepped around and every one that was refused before it got a claim.
func noteUncommitted(ctx context.Context) {
	if n, ok := ctx.Value(claimKey{}).(*claimNote); ok {
		n.uncommitted = true
	}
}

// markEffect asks the request's transaction to write this command's effect marker as
// part of itself, on the way to a COMMIT.
//
// The marker has to be inside the transaction to be worth anything — see Store.Mark —
// and a transaction that never opened wrote nothing to mark, so asking costs nothing
// there and returns nothing. A request that claimed no key leaves no marker either:
// the marker exists so that one key's next press can read what its last press's COMMIT
// did, and there is no such press on a write that was never keyed.
func markEffect(ctx context.Context, p *db.Pending) error {
	n, ok := ctx.Value(claimKey{}).(*claimNote)
	if !ok || n.store == nil || n.claim == uuid.Nil {
		return nil
	}
	tx, open := p.Opened()
	if !open {
		return nil
	}
	return n.store.Mark(ctx, tx, n.scope, n.claim)
}

// keptHeaders are the response headers an answer is not the same answer without.
//
// Three of them. `Location` is the 303 a browser follows and `HX-Redirect` is the
// same instruction as htmx reads it (html.go's Redirect chooses between them by the
// request in front of it), so a keyed form post that answered by sending the caller
// to the page its write finished at still sends them there on the retry: both are
// navigation, and a redirect without its destination is a status with nowhere to go.
// The third is `RateLimit-Policy`, kept because it is the one limit field that never
// ages — a name, a quota and a window are the same on every answer under them, which
// is the property draft-ietf-httpapi-ratelimit-headers asks that field to keep — and
// because the bytes the deciding answer stated are the only complete account of the
// ceilings that answer named: a door may name a ceiling it did not spend, and the
// readings kept beside an answer name none of it. Its two counting siblings are absent
// from this list and present on a replay all the same, because a countdown is restated
// rather than repeated (sayRecorded).
//
// Every other header is deliberately absent:
// `Set-Cookie` is a credential minted for one exchange, and a remembered one would
// be a live session key sitting in a row for a day and a dead one the moment the
// session behind it was revoked — which is why a route whose answer is a credential
// declares that it takes no key at all (CredentialExtension) rather than promising a
// replay that cannot sign anybody in. `Cache-Control` and `Vary` describe this
// exchange rather than the answer. `Content-Type` has its own field because it is the one the row is read into. A
// header not on this list is a header a replay answers without, which is the trade
// the memory makes on purpose: keep what the answer does, not how it was carried.
var keptHeaders = []string{"Location", "HX-Redirect", RateLimitPolicyHeader, OperationIDHeader, OperationLocationHeader}

// keepHeaders reads the headers worth keeping out of the response the caller was
// finally given, in the order the list names them, with the value the writer
// actually sent.
func keepHeaders(h http.Header) []idempotency.Header {
	var kept []idempotency.Header
	for _, name := range keptHeaders {
		if v := h.Get(name); v != "" {
			kept = append(kept, idempotency.Header{Name: name, Value: v})
		}
	}
	return kept
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
func (a *API) complete(ctx context.Context, n *claimNote, status int, header http.Header, body []byte, held bool, counted []measured) {
	if n == nil || n.store == nil || n.claim == uuid.Nil {
		return
	}
	if n.uncommitted {
		// What the caller was given describes a transaction this process cannot place.
		// Recording it would spend the key on an answer to nothing; leaving the record
		// begun hands it to the effect marker, which is the one row that knows whether
		// this command's COMMIT reached the database. This is the one branch here that
		// logs at Info: a caller that gets a 500 here will retry, and the retry is
		// answered by the resolution rather than by this line.
		a.rlog(ctx).InfoContext(ctx, "httpx: the keyed write's transaction did not commit; its answer is not remembered",
			"method", n.scope.Method, "path", n.scope.Route, "status", status)
		return
	}
	now := n.ctx
	if now == nil {
		now = ctx
	}
	// This reading dates the record, and nothing else: the windows its limits name were
	// closed when their readings arrived, because a handler may work for seconds after
	// the statement that decided the answer (see recorded).
	at := a.opts.Clock().UTC()
	answer := idempotency.Answer{
		Status: status, ContentType: header.Get("Content-Type"), Body: body,
		Headers: keepHeaders(header), Limits: recorded(counted),
	}
	if n.noReplay || !held || len(answer.Body) > maxRecordedAnswer {
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
	err := n.store.Complete(detached, n.scope, n.claim, answer, at)
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
// spends its key rather than replaying it; the document's description of the header
// names the body ceiling only, and README states the spent answer and its 409.
func declaresKey(op *huma.Operation) bool {
	if op == nil {
		return false
	}
	switch op.Method {
	case http.MethodPost, http.MethodPatch:
	default:
		return false
	}
	return declaresIdempotencySurface(op) && !mintsACredential(op)
}

// CredentialExtension marks an answer containing a newly minted credential.
// The kernel tracks its intent but never stores the credential for replay.
// A keyed retry is spent and cannot mint a second credential.
const CredentialExtension = "x-platformkit-mints-a-credential"

// MintsACredential is what a route whose answer is a credential it just made puts in
// its Extensions, beside EventsExtension: the answer is spendable only by the exchange
// that received it, so no retry of it is answerable from a memory and none is promised.
func MintsACredential() (string, any) { return CredentialExtension, true }

// mintsACredential reads that declaration back, and is the one answer to the question
// for both sides of it: declaresKey asks it of the document, and the guard asks it of
// the request, so the promise and the wire cannot disagree.
func mintsACredential(op *huma.Operation) bool {
	if op == nil || op.Extensions == nil {
		return false
	}
	_, mints := op.Extensions[CredentialExtension]
	return mints
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
var keyDescription = "Replays this write for 24 hours. A retry with byte-identical request and body gets the answer the first attempt was given; the same key with another body, path or query is refused 422, and the same key while the first attempt is still running is refused 409. Optional: a write without a key receives a new operation identity. Unresolved and approval-pending identities remain spent after answer expiry until their owner completes them. A keyed body is bounded to " + strconv.Itoa(maxKeyedBody) + " bytes; a larger write is refused 413 rather than made unremembered. A UUID is the usual value."

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
