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
	maxKeyLength = operation.MaxTokenLength
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
	return request.ValidToken(inner)
}

// keyRule names the rule in the caller's terms: the answer they can act on is a
// UUID, which is what the draft recommends for exactly this reason.
const keyRule = "an idempotency key is 1 to 128 characters of the RFC 9110 token set (a UUID is the usual choice)"

// addressOf preserves the escaped path used for routing and request identity.
// Decoded paths can collapse distinct captured resources; differing escaped
// spellings safely produce different fingerprints.
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

// idempotency admits keyed writes after authentication, authorization and bounded
// body handling. Replays recheck current grants. It stays inside the request
// transaction; respond records the final answer after commit or rollback.
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
	body, ok := a.keyedBody(ctx, r)
	if !ok {
		return
	}

	scope := keyedScope(ctx, r, key, body)

	v, err := store.Claim(context.WithoutCancel(ctx.Context()), scope, time.Now().UTC())
	if err != nil {
		// Fail closed when the claim cannot be read; otherwise a retry could
		// repeat work the store already recorded.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the idempotency record could not be read; the write is refused",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuse(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable+": this write could not be remembered, so it was not made")
		return
	}

	switch v.Kind {
	case idempotency.First:
		// Persist Begin before noteClaim or the handler. An unbegun claim stays
		// retryable when storage fails, and no write runs without its durable mark.
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

// replay returns the stored answer with Content-Type and keptHeaders, restating
// rate-limit countdowns for this attempt. Cookies and exchange-specific cache
// headers are excluded. The request ID names this retry even when the stored
// problem body names the original request.
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
	// The answer is remembered as the bytes one representation produced. A retry
	// that asks for a different one is refused rather than served the old spelling.
	if !a.replayRepresentation(ctx, stored.ContentType, stored.Status) {
		return
	}
	r, w := humachi.Unwrap(ctx)
	if stored.ContentType != "" {
		w.Header().Set("Content-Type", stored.ContentType)
	}
	// The kept headers go on before the status line, as every header does: a 303
	// replayed without its Location is a status with nothing to act on, and the
	// retry of a form that worked would arrive somewhere it could not go.
	//
	// Set and not added: a guard above this one may have stated one of these names for
	// this exchange already — the public write limit states its policy line on every
	// reply it applies to — and an answer remembered under its ceilings is stated once,
	// as it was decided, rather than twice under two readings of one ceiling.
	for _, h := range stored.Headers {
		w.Header().Set(h.Name, h.Value)
	}
	// The ceilings the write was refused under go on the replay too, with the one
	// number that ages worked out again, and they are the whole of the line: the guard
	// above this one spends its own counter on the way down to the memory, and that
	// reading belongs to the exchange asking again, not to the answer it is being given
	// (sayRecorded). The policy line is already on its way: the answer kept it among its
	// own headers just above. The reading is the one the deciding counter gave, and the
	// wait is what is left of the window that refusal landed in. A replay that said
	// nothing let respond.go's floor stand in — a minute for a window of an hour — which
	// is a guess where the answer had a number.
	sayRecorded(w.Header(), stored.Limits, stored.Status == http.StatusTooManyRequests, time.Now().UTC())
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

// keptHeaders preserves redirect destinations and the fixed rate-limit policy.
// Countdowns are restated separately; Content-Type has its own stored field.
// Cookies and exchange-specific cache headers must never be replayed.
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

// complete records the final delivered response, including 5xx, after transaction
// finalisation. Undelivered answers leave their claim unresolved until its lease
// expires. Recording uses the claim's detached context so client cancellation
// cannot erase an answer already sent.
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

func (a *API) keyedBody(ctx huma.Context, r *http.Request) ([]byte, bool) {
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
		return nil, false
	}
	if len(body) > maxKeyedBody {
		a.refuse(ctx, http.StatusRequestEntityTooLarge, CodeIdempotencyTooLarge+": a keyed write is remembered up to "+strconv.Itoa(maxKeyedBody)+" bytes")
		return nil, false
	}
	// Restore both Huma's request and the carried request before the handler.
	// WithContext copies the Body field, so either reader may need replacement.
	r.Body = io.NopCloser(bytes.NewReader(body))
	if carried, ok := RequestFrom(ctx.Context()); ok && carried != nil && carried != r {
		carried.Body = io.NopCloser(bytes.NewReader(body))
	}

	return body, true
}

func keyedScope(ctx huma.Context, r *http.Request, key string, body []byte) idempotency.Scope {
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
	return idempotency.Scope{
		Tenant: t.ID,
		Caller: caller,
		Method: ctx.Method(),
		Route:  routeOf(ctx),
		Key:    key,
		Fingerprint: fingerprint(ctx.Method(), routeOf(ctx), addressOf(r),
			r.URL.RawQuery, r.Header.Get("Content-Type"), body),
	}

}
