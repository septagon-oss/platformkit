package httpx

// idempotency.go makes one submission of a command happen once, whichever end of
// the network forgot about it.
//
// A write whose response is lost is a write the client cannot retry: it does not
// know whether the command ran, and the answer every client has written for
// itself — keep the body, show a sentence, hand the person a button — costs a
// thousand lines of browser code apiece and gets it wrong in a new way each time.
// The kernel can answer instead, because the kernel is the one that knows: a
// command that declares the header gets its key, its caller and a hash of its
// bytes written down before it runs, and the response the caller finally got
// written down beside them. The same key with the same bytes then answers with
// that response and runs nothing; the same key with other bytes is a different
// command wearing an old name and is refused; and a repeat that arrives while the
// first is still running is refused too, because two of one command are not one
// of them twice.
//
// Two middlewares, for one reason: the claim needs the principal, which only
// exists past the tenant, the transaction and the guards, and the record needs
// the commit to be over, which is only true outside the transaction. One
// middleware cannot stand on both sides of it. See New for where each sits.
//
// What this does not buy is stated where it is read: an idempotent command stops
// a double transition, not a lost update, and a key replayed after its day is a
// fresh command nothing can tell from the first (docs/adr/0016). The command's
// own expected revision is what refuses two different writes.
//
// Nothing here infers that a command finished from how long ago it started. The
// kernel cannot see another process's request, so an in-flight claim it cannot
// reach is a claim it must assume is still running: the repeat is refused, and the
// scheduled purge is what frees the key of a process that died (see the note on
// expires_at in claimIdempotency). Refusing a retry of a command that is really
// gone costs a person one more press, some minutes later; taking over a claim that
// is not really gone costs them the command twice.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
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
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// IdempotencyKeyHeader is the request header that names one submission of one
// command. The name is the IETF HTTPAPI draft's, so that a client written against
// the draft arrives with the header this kernel reads rather than a near-miss.
const IdempotencyKeyHeader = "Idempotency-Key"

// IdempotencyReplayHeader is set on an answer the kernel handed back from the
// stored response. It is not the fact — the same answer is the fact — but a
// caller that wants to know whether its command ran this time asks for this.
const IdempotencyReplayHeader = "Idempotency-Replay"

// IdempotencyRefusalHeader names, on the four refusals this gate makes, which of
// them it was. It exists for one reason, and the reason is that a person cannot be
// shown a code: ui/page renders the refusal's sentence in their own language, so
// the machine reading the answer — the command controller, a client of the API —
// cannot tell "this key is spent" from "the server drew your form again" once the
// English has replaced the code. Both are 422s; one means send it again and the
// other means never send it again.
const IdempotencyRefusalHeader = "Idempotency-Refusal"

// idempotencyTable is the one this package owns. See migrations/000042_idempotency.
const idempotencyTable = "platformkit_idempotency"

const (
	// maxStorableResponse is the largest answer this mechanism will keep. A
	// command answers with the entity's JSON or with nothing, so 64 KiB is far
	// past the promise and far below the 2 MiB the buffer holds; and a table of
	// a day of commands, each bounded, is a table somebody can explain. An
	// answer past it is not truncated and not dropped: the row says it ran, and
	// a repeat is told to read the result. TestAStoredResponseIsSmallerThanTheBuffer
	// keeps this below maxBuffer.
	maxStorableResponse = 1 << 16
	// claimBudget bounds the two statements of the mechanism, for the reason
	// kit/limit gives for its own: neither is allowed to turn a database that has
	// stopped answering into a request that never ends.
	claimBudget = 2 * time.Second
)

// replayHeaders are the response headers a stored answer carries back, beside the
// status and the body. They are the three a client acts on — htmx follows
// HX-Redirect, a browser follows Location, and Retry-After is the wait a refusal
// names — and nothing else: notably not Set-Cookie, which no surface below a
// session should be handing a replay, and not X-Request-ID, which stays the
// current request's so that a person quoting a replay lands on a log line they can
// find. Content-Type travels in its own column rather than twice.
var replayHeaders = []string{"HX-Redirect", "Location", "Retry-After"}

// idempotencyToken is minted for the reason every cross-tenant transaction here
// has to be minted for one: the only door to this table is the request gate, which
// is above any tenant's transaction by construction. Named at construction, as
// kit/limit names its own, so the log says which control plane opened what.
var idempotencyToken = syscap.NewSystemToken("idempotency records")

// DeclareIdempotency marks an operation idempotent-by-key.
//
// The marking is the OpenAPI header parameter and nothing else: the parameter is
// both what the document publishes and what the request gate reads, so there is no
// second channel to keep in step (the argument kit/httpx makes about events).
// Declaring it on a route whose handler is not safe to run twice is the module's
// decision; the kernel only refuses the three declarations that cannot mean what
// they say, which ValidateDeclarations does at boot.
func DeclareIdempotency(op *huma.Operation) {
	for _, p := range op.Parameters {
		if p != nil && p.Name == IdempotencyKeyHeader && p.In == "header" {
			return
		}
	}
	op.Parameters = append(op.Parameters, &huma.Param{
		Name:     IdempotencyKeyHeader,
		In:       "header",
		Required: false,
		Description: "One submission of this command. Send the same key with the same body to retry it, " +
			"and the first submission's answer comes back; the same key with a different body is a " +
			"different command and is refused. A key is a lowercase UUID, and it is kept for a day.",
		Schema: &huma.Schema{Type: huma.TypeString, Format: "uuid"},
	})
}

// declaresIdempotency is the request gate's reading of that declaration.
func declaresIdempotency(op *huma.Operation) bool {
	if op == nil {
		return false
	}
	for _, p := range op.Parameters {
		if p != nil && p.Name == IdempotencyKeyHeader && p.In == "header" {
			return true
		}
	}
	return false
}

// idempotencyClaim is the whole identity of a request: two requests are the same
// request when all five fields match. The tenant and the caller come from the
// context and from nowhere else, so a key cannot be aimed at another customer or
// another account; the operation is the mounted pattern, for the reason routeOf
// gives — a set of keys anyone can inflate is a table anyone can fill; and the
// concrete path and query live in the hash, so the same key at another item of the
// same route is a reused key rather than another item's answer.
type idempotencyClaim struct {
	tenant    uuid.UUID
	actor     uuid.UUID
	operation string
	key       string
	hash      [sha256.Size]byte
	request   string
}

// idempotencyHolder is where a request that owns its claim says so, on the
// context, for the middleware on the way back out. A pointer, because the two
// halves of the mechanism are on opposite sides of the transaction.
type idempotencyHolder struct {
	claim *idempotencyClaim
	// uncommitted is the transaction middleware's verdict, which the response
	// cannot carry. See noteUncommitted for why the record step needs it and
	// cannot get it from the buffered status.
	uncommitted bool
}

type claimKey struct{}

func holderFrom(ctx context.Context) *idempotencyHolder {
	h, _ := ctx.Value(claimKey{}).(*idempotencyHolder)
	return h
}

// noteUncommitted tells the record step that this request's transaction did not
// commit, so the answer the handler wrote must not settle the key.
//
// The response cannot carry this fact, because one of the three ways a commit fails
// has no honest response to write: a caller that went away mid-commit leaves the
// buffered 200 in place — `hungUp` keeps it precisely because nobody is left to be
// told anything — and those are the bytes `idempotencyRecord` reads. The caller that
// hung up is the caller this mechanism exists for, and it comes back with the same
// key; settling the key on that 200 tells it its command ran when the database
// rolled it back, and its retry never runs. So the middleware that decides commit or
// rollback says so here, on the holder the record step already reads, rather than
// rewriting a response nobody is waiting for.
// TestACommandWhoseCallerHungUpBeforeTheCommitRunsOnItsRetry is the case.
func noteUncommitted(ctx context.Context) {
	if h := holderFrom(ctx); h != nil {
		h.uncommitted = true
	}
}

// idempotency is the claim: it runs the command, hands back a stored answer, or
// refuses — and for a request that will not run, it never calls next.
func (a *API) idempotency(ctx huma.Context, next func(huma.Context)) {
	op := ctx.Operation()
	if !declaresIdempotency(op) {
		next(ctx)
		return
	}
	key := ctx.Header(IdempotencyKeyHeader)
	if key == "" {
		// The header is optional, which is what keeps a client that predates this
		// working: a route that declares the key and is not given one simply runs,
		// exactly as it did before. A malformed key is the opposite case — it names
		// an intent to be idempotent and gets a refusal, because retrying the same
		// malformed token forever is a client that never learns.
		next(ctx)
		return
	}
	actor, ok := tenancy.ActorFrom(ctx.Context())
	if !ok {
		// A key scopes to a caller. With no caller there is nobody to scope it to:
		// every anonymous visitor of the tenant would share one row, which is a
		// replay table one visitor can aim at another's. ValidateDeclarations
		// refuses the declaration that could reach here on purpose, so arriving
		// means a route lost its principal between the guard and this gate — and
		// running the command anyway is the silent double-apply the header exists to
		// prevent, answered with a 200 that promises a key nothing was written for.
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: an idempotency key arrived with no principal",
			"method", ctx.Method(), "path", ctx.URL().Path)
		a.refuseGate(ctx, http.StatusForbidden, CodeAnonymous+": this command needs a caller to scope its key to")
		return
	}
	tenant, ok := tenancy.FromContext(ctx.Context())
	if !ok {
		// The same reasoning with the other half of the identity: a key scoped to an
		// actor but to no tenant is one row two customers could each read.
		a.rlog(ctx.Context()).WarnContext(ctx.Context(), "httpx: an idempotency key arrived with no tenant",
			"method", ctx.Method(), "path", ctx.URL().Path)
		a.refuseGate(ctx, http.StatusForbidden, CodeNoTenant+": this command's key has no tenant to scope it to")
		return
	}
	holder := holderFrom(ctx.Context())
	if holder == nil {
		// No record step for this request, so a claim would settle never: the key
		// would be an in-flight marker for five minutes and then a free second run.
		next(ctx)
		return
	}
	if !idempotencyKeyWellFormed(key) {
		a.refuseGate(ctx, http.StatusUnprocessableEntity, CodeIdempotencyKeyInvalid+": the key must be a lowercase UUID")
		return
	}

	body, err := readBody(ctx)
	if err != nil {
		// The body could not be read whole, so its hash could not be taken. huma
		// reads the same bytes next and gives this request the answer a bad body
		// deserves; nothing was claimed, so nothing was recorded.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: an idempotent command's body could not be read",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		next(ctx)
		return
	}
	r, _ := humachi.Unwrap(ctx)
	c := idempotencyClaim{
		tenant: tenant.ID, actor: actor,
		operation: ctx.Method() + " " + routeOf(ctx),
		key:       key,
		hash:      hashRequest(ctx.Method(), r.URL.RequestURI(), r.Header.Get("Content-Type"), body),
		request:   requestIDFrom(ctx.Context()),
	}
	answer, err := a.claimIdempotency(ctx.Context(), c)
	if err != nil {
		// Fail closed, which is deliberately the opposite of the public write
		// limit beside this middleware: a limit that cannot answer lets a request
		// through because the alternative is a form that stops working, and a key
		// that cannot be claimed cannot promise anything, so the command the
		// caller is about to run would be a command with no answer kept for it. The
		// command needs the same database; refusing costs a request that was
		// going to fail, and nothing was written.
		a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: the idempotency claim could not be written",
			"method", ctx.Method(), "path", ctx.URL().Path, "error", err)
		a.refuseGate(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable+": the command could not be claimed, and nothing ran")
		return
	}
	switch {
	case answer.held != nil:
		a.replay(ctx, answer.held, c)
	case answer.refuse != 0:
		if answer.retryAfter > 0 {
			ctx.SetHeader("Retry-After", strconv.Itoa(answer.retryAfter))
		}
		a.refuseGate(ctx, answer.refuse, answer.code)
	default:
		// This request owns the key. Its record step is what makes that a promise.
		holder.claim = &c
		next(ctx)
	}
}

// idempotencyRecord writes what the caller was finally given, or releases the
// claim. It sits immediately inside respond for the reason respond states: that is
// the one place that knows what the client was actually handed, after the commit,
// after any recovery, after any reset that turned a held 200 into a 500. The
// commit's own verdict is not in what was handed over, so the transaction
// middleware puts it on the holder this reads — see noteUncommitted.
//
// The record is a deferred step rather than a following one, because a panic that
// unwinds through here never reaches a statement written after the call. That is
// the one outcome that must not be left unsettled: the transaction below this
// middleware rolled back on its way out, nothing ran, and a claim left standing
// would refuse the retry of a command that produced no row — for five minutes, or
// until a purge, which is a person staring at "another request is still running"
// about a request that stopped a second ago. TestAPanickedCommandReleasesItsKeyAfterRollback
// is the case; http.ErrAbortHandler is the one panic that settles nothing, because
// it means "this response is being dropped", not "this work failed".
func (a *API) idempotencyRecord(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(context.WithValue(r.Context(), claimKey{}, &idempotencyHolder{}))
		defer func() {
			if v := recover(); v != nil {
				if v != http.ErrAbortHandler {
					a.forgetClaim(r.Context(), holderFrom(r.Context()))
				}
				panic(v)
			}
			holder := holderFrom(r.Context())
			if holder == nil || holder.claim == nil {
				return
			}
			b, ok := bufferFrom(r.Context())
			if !ok {
				return
			}
			status, body, held := b.status, b.body.Bytes(), !b.begun()
			if status == 0 {
				status = http.StatusOK
			}
			if err := a.recordIdempotency(r.Context(), *holder.claim, status, b.Header(), body, held, holder.uncommitted); err != nil {
				// The command ran and its answer was not kept. The caller has its
				// response; the next one gets a fresh claim in five minutes rather than
				// a stored response it was never given, which is the safe direction: a
				// row left unsettled refuses a repeat, and a row that lies about an
				// answer replays one.
				a.rlog(r.Context()).ErrorContext(r.Context(), "httpx: the idempotency record could not be written",
					"method", r.Method, "path", r.URL.Path, "error", err)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// forgetClaim releases the key of a request that is leaving by the panic door. The
// statement is the one recordIdempotency already uses for a 5xx and for a
// transaction that did not commit, and it runs for the same reason: an answer
// nobody received is not an answer to remember, and work that rolled back must run
// again. Failure is a log line and nothing more — the request is already failing,
// and a claim that outlives it is the smaller of the two faults.
func (a *API) forgetClaim(ctx context.Context, holder *idempotencyHolder) {
	if holder == nil || holder.claim == nil {
		return
	}
	if err := a.recordIdempotency(ctx, *holder.claim, http.StatusInternalServerError, nil, nil, false, true); err != nil {
		a.rlog(ctx).ErrorContext(ctx, "httpx: the idempotency claim of a panicked command could not be released",
			"error", err)
	}
}

// recordIdempotency is the decision, made as one statement against one table: either
// release the claim, or settle it with the answer.
//
//   - A 5xx, in any form — the handler's own, the 500 the transaction substitutes
//     when the commit failed, the 500 a panic recovery wrote — deletes the claim.
//     A refusal nobody received is not an answer to remember, and a retry of work
//     that rolled back must run: house rule 9 said backwards.
//   - A transaction that did not commit deletes it too, whatever the buffered
//     response says. This is the same fact as the bullet above, read from the side
//     that cannot answer: a commit that failed on a request whose caller had already
//     gone away leaves a 200 nobody read behind, and `uncommitted` is how the record
//     step learns the work is not in the database. See noteUncommitted. The panic
//     that never reached a response at all takes this same branch — see idempotencyRecord.
//   - A response under 500 that never reached the wire early and fits is stored
//     whole, with its status, its Content-Type and the three headers a client acts on.
//     An answer with no body — the redirect a page's own command answers with — is
//     stored empty rather than as nothing: what makes a row replayable is that
//     `response` is not NULL, and a 204 simply has no bytes to keep.
//   - A response under 500 that is too big, or that already began (a Flush, a
//     body past the buffer's bound), settles with a NULL response. It ran, and its
//     answer is gone; releasing the claim instead is what makes a retry double-apply.
func (a *API) recordIdempotency(ctx context.Context, c idempotencyClaim, status int, header http.Header, body []byte, held, uncommitted bool) error {
	return a.detached(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		if uncommitted || status >= http.StatusInternalServerError {
			return tx.DB().Exec("DELETE FROM "+idempotencyTable+
				" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
				c.tenant, c.actor, c.operation, c.key).Error
		}
		if !held || len(body) > maxStorableResponse {
			return tx.DB().Exec("UPDATE "+idempotencyTable+
				" SET settled = true, status = ?, response = NULL, expires_at = claimed_at + interval '24 hours'"+
				" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
				status, c.tenant, c.actor, c.operation, c.key).Error
		}
		// A held answer is stored as bytes even when it has none: what a repeat reads
		// to decide it may replay is `response IS NOT NULL`, and the redirect a page's
		// own command answers with has no body to lose.
		stored := body
		if stored == nil {
			stored = []byte{}
		}
		return tx.DB().Exec("UPDATE "+idempotencyTable+
			" SET settled = true, status = ?, content_type = ?, headers = ?::jsonb, response = ?::bytea,"+
			" request_id = ?, expires_at = claimed_at + interval '24 hours'"+
			" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
			status, header.Get("Content-Type"), replayHeaderJSON(header), stored, c.request,
			c.tenant, c.actor, c.operation, c.key).Error
	})
}

// replay writes the stored answer as it was written the first time, plus the one
// header that says it is a replay. It is the bytes and not a re-render: what the
// caller is owed is the answer it asked for, including the redirect it acted on.
func (a *API) replay(ctx huma.Context, h *heldResponse, c idempotencyClaim) {
	_, w := humachi.Unwrap(ctx)
	for _, name := range replayHeaders {
		if v := h.headers[name]; v != "" {
			w.Header().Set(name, v)
		}
	}
	if h.contentType != "" {
		w.Header().Set("Content-Type", h.contentType)
	}
	w.Header().Set(IdempotencyReplayHeader, "true")
	// The status goes to the huma context as well as to the writer, for the reason
	// refuse gives: the transaction middleware decides commit or rollback on what
	// this response carries, and a verdict that reached only the writer is read as
	// nobody deciding.
	ctx.SetStatus(h.status)
	w.WriteHeader(h.status)
	if len(h.body) > 0 {
		_, _ = w.Write(h.body)
	}
	a.rlog(ctx.Context()).InfoContext(ctx.Context(), "httpx: a command was replayed from its idempotency key",
		"method", ctx.Method(), "path", ctx.URL().Path, "status", h.status,
		"original_request", h.requestID, "request", c.request)
}

// refuseGate is refuse with the code stated as a header as well as in the body.
// The detail already begins with the code — that is the shape every refusal in
// this package has — and this puts the same word where a reader that is not
// parsing a sentence can find it. See IdempotencyRefusalHeader.
func (a *API) refuseGate(ctx huma.Context, status int, detail string) {
	code, _, _ := strings.Cut(detail, ":")
	ctx.SetHeader(IdempotencyRefusalHeader, code)
	a.refuse(ctx, status, detail)
}

// heldResponse is a stored answer, or the fact that there is none.
type heldResponse struct {
	status      int
	contentType string
	headers     map[string]string
	body        []byte
	// holds is `response IS NOT NULL`, which is the only reading of "was an answer
	// kept" that survives an answer with nothing in it: a page's own command
	// redirects with a header and no body at all, and its bytes are nil because they
	// are empty, not because there is no answer to replay.
	holds     bool
	requestID string
	settled   bool
	expired   bool
	hash      []byte
}

// claimIdempotency is the three-way decision, in one transaction of its own: run,
// replay, or refuse. Detached, because an in-flight marker written inside the
// request's own transaction is invisible to the concurrent repeat that exists to
// be refused — and the loser of that race is the whole point of it.
func (a *API) claimIdempotency(ctx context.Context, c idempotencyClaim) (idempotencyAnswer, error) {
	var out idempotencyAnswer
	err := a.detached(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		for range 2 {
			// Twice, because the row can stop existing between the two statements: the
			// claim of a request that failed is deleted, an expired answer is deleted
			// below, and a purge can take either between the failed INSERT and the read.
			// Neither is a fault of this request's, and neither may be answered with a
			// refusal of an in-flight command that is not in flight.
			res := tx.DB().Exec("INSERT INTO "+idempotencyTable+
				" (tenant_id, actor_id, operation, key, request_hash, claimed_at, expires_at)"+
				" VALUES (?, ?, ?, ?, ?, now(), now() + interval '5 minutes')"+
				" ON CONFLICT (tenant_id, actor_id, operation, key) DO NOTHING",
				c.tenant, c.actor, c.operation, c.key, c.hash[:])
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				out.run = true
				return nil
			}
			h, err := readHeld(tx, c)
			if err != nil {
				return err
			}
			if h == nil {
				continue
			}
			if h.settled && h.expired {
				// The answer's own day is over: the window the response is kept for has
				// passed, and past it a key is a fresh command (docs/adr/0016). Waiting for
				// the scheduled purge would make the promise the clock says rather than the
				// one the document says, so the row that has nothing left to give goes here,
				// in the same statement that is about to be asked again. An *unsettled* row
				// whose deadline passed is a different thing and gets a different answer:
				// see the note on expires_at below.
				if err := tx.DB().Exec("DELETE FROM "+idempotencyTable+
					" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ? AND settled",
					c.tenant, c.actor, c.operation, c.key).Error; err != nil {
					return err
				}
				continue
			}
			out = settledAnswer(c, h)
			return nil
		}
		return errors.New("httpx: the idempotency claim vanished twice")
	})
	if err != nil {
		return idempotencyAnswer{}, err
	}
	return out, nil
}

// settledAnswer is the whole of the answer for a settled row, and for one still in
// flight: an idempotent command's status is what the second caller is being told,
// so it is decided here rather than in three places.
//
// The `!settled` branch is the cure for the one case a clock cannot see. A claim
// that is not settled says one of two things: somebody is running this command
// now, or the process that claimed it died mid-flight. The kernel has no way to
// tell them apart — its own transactions are committed and detached precisely so
// that a repeat on another connection can see the first one — and the two answers
// it could give are not equally bad. Taking over an ageing marker and running the
// command again applies a write twice whenever the first owner was merely slow:
// TestARunningCommandIsNotAppliedTwiceWhenItsClaimAges is that case, and no bound
// someone can write down is longer than the request that can outlive it. Refusing
// costs the dead process's caller a Retry-After and one more press after the purge,
// which is what expires_at and the scheduled job are for. So the refusal is the
// answer, always, and age decides nothing on this path.
func settledAnswer(c idempotencyClaim, h *heldResponse) idempotencyAnswer {
	switch {
	case !h.settled:
		return inProgress()
	case !bytes.Equal(h.hash, c.hash[:]):
		return idempotencyAnswer{refuse: http.StatusUnprocessableEntity, code: CodeIdempotencyKeyReuse + ": this key was already used for a different request"}
	case !h.holds:
		return idempotencyAnswer{refuse: http.StatusConflict, code: CodeIdempotencyResponseNotHeld + ": this command ran; its response is not held; read the result rather than sending it again"}
	default:
		return idempotencyAnswer{held: h}
	}
}

// inProgress is the refusal of a command somebody else is running: correctable by
// waiting and by nothing else, and never by rewriting the other request's outcome.
func inProgress() idempotencyAnswer {
	return idempotencyAnswer{refuse: http.StatusConflict, code: CodeIdempotencyInProgress + ": another request with this key is still running", retryAfter: 2}
}

// reclaimSQL is gone, and its absence is the point: no statement in this package
// takes over another request's claim. The recovery of a claim whose owner died is
// the purge's job, which deletes a row only once its expires_at has passed and so
// cannot delete one that is still being answered.

// idempotencyAnswer is one of three things: run, replay, or refuse.
type idempotencyAnswer struct {
	run        bool
	held       *heldResponse
	refuse     int
	code       string
	retryAfter int
}

// readHeld reads the row that is already there. No row is not an error: it is a
// claim that ended between our failed INSERT and this read, which the caller
// answers by claiming again.
//
// The last column is the row's own deadline, read on the database clock like
// everything else here. What it means depends on the row's state, and the two
// readings must not be confused: on a settled row it is the end of the window the
// answer is kept for, which this request enforces for itself; on an unsettled one
// it is the point at which the *purge* may take the marker of a command nobody is
// answering any longer, which is not a fact a request may act on as if it were
// knowledge of whether a process is alive.
func readHeld(tx db.Tx[db.System], c idempotencyClaim) (*heldResponse, error) {
	rows, err := tx.DB().Raw("SELECT settled, status, content_type, headers, response, response IS NOT NULL, request_hash, request_id,"+
		" now() > expires_at FROM "+idempotencyTable+
		" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
		c.tenant, c.actor, c.operation, c.key).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var (
		h       heldResponse
		headers []byte
	)
	if err := rows.Scan(&h.settled, &h.status, &h.contentType, &headers, &h.body, &h.holds, &h.hash, &h.requestID, &h.expired); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(headers) > 0 {
		if err := json.Unmarshal(headers, &h.headers); err != nil {
			return nil, err
		}
	}
	return &h, nil
}

// PurgeIdempotency deletes the answers a day old. As with limit.Purge it is a
// scheduled job and never a side effect of a claim: a request that also deleted
// somebody else's row is a request paying for traffic it is not refusing, and a
// table nobody empties grows at somebody else's rate. The expires_at index above
// is this statement's query.
func PurgeIdempotency(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, idempotencyToken, func(_ context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Exec("DELETE FROM " + idempotencyTable + " WHERE expires_at < now()").Error; err != nil {
			return errors.New("httpx: purge idempotency: " + err.Error())
		}
		return nil
	})
}

// replayHeaderJSON is the stored half of the allowlist, as the jsonb column wants
// it. Nothing outside the list is stored, and nothing but these four ever reaches
// a caller again: a stored Set-Cookie would be a session handed out twice, and a
// stored X-Request-ID would be a log line that does not exist.
func replayHeaderJSON(h http.Header) []byte {
	out := make(map[string]string, len(replayHeaders))
	for _, name := range replayHeaders {
		if v := h.Get(name); v != "" {
			out[name] = v
		}
	}
	b, err := json.Marshal(out)
	if err != nil {
		// A map of five strings cannot fail to marshal; if it ever can, the answer
		// is a row with no headers rather than a command that loses its response.
		return []byte("{}")
	}
	return b
}

// readBody takes the command's bytes once, and hands the same bytes on to huma.
// The request the handler will read is the one this middleware was handed, so the
// replacement is written back onto it rather than onto a copy: every copy huma
// makes from here down carries the reader that still has the body in it.
//
// The ceiling is the one bodies.go would have applied and cannot apply here —
// bodies.go wraps a different copy of the request, and this middleware consumes
// the bytes before it. http.MaxBytesReader rather than a reader of this package's
// own, for the reason that file gives: the server learns the request was too
// large, so the connection closes instead of half-reading.
func readBody(ctx huma.Context) ([]byte, error) {
	r, _ := humachi.Unwrap(ctx)
	if r.Body == nil {
		return nil, nil
	}
	_, w := humachi.Unwrap(ctx)
	limited := http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		// Whatever was read stays read, and the caller that reads the rest gets the
		// same error: this is the answer a body over the ceiling was always going
		// to get, from the same reader.
		r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), limited))
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// hashRequest is the fingerprint of "the same request". The concrete path and
// query are inside it, so one key used against two different items of the same
// route is a reused key and not another item's answer; the method and the
// Content-Type are inside it, so a body re-encoded as another media type is a
// different command.
func hashRequest(method, uri, contentType string, body []byte) [sha256.Size]byte {
	h := sha256.New()
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(uri))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(contentType))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write(body)
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

// idempotencyKeyWellFormed accepts one shape: a canonical, lowercase UUID. A key
// that is not one is a client that did not mint it the way the header means, and
// the two shapes a mistake actually takes — upper case, and a name somebody typed
// instead of a token — are refused rather than silently canonicalised, because a
// key that means the same thing in two spellings is a key whose two senders think
// they have different intents.
func idempotencyKeyWellFormed(key string) bool {
	if len(key) != 36 {
		return false
	}
	u, err := uuid.Parse(key)
	return err == nil && u.String() == key
}

// detached runs one statement of this mechanism on a context of its own, for the
// three reasons kit/limit gives for the same shape: the claim must survive the
// rollback of the request that made it, the record must reach the row after the
// caller hung up, and neither may turn a database that stopped answering into a
// request that never ends.
func (a *API) detached(ctx context.Context, fn func(context.Context, db.Tx[db.System]) error) error {
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), claimBudget)
	defer cancel()
	return db.RunSystem(detached, a.opts.Conn, idempotencyToken, fn)
}
