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
// reach is a claim it must assume is still running and its repeat is refused (see
// the note on expires_at in claimIdempotency). Refusing a retry of a command that
// is really gone costs a person one more press, some minutes later; taking over a
// claim that is not really gone costs them the command twice.
//
// "Still running" is a fact the row can hold rather than one the clock has to
// guess at, and it does: the request that owns a claim renews its lease while it
// works, so a claim with a current lease is one somebody is answering at any age,
// and a lapsed lease is the one claim the kernel can prove nobody is answering. The
// lease is why the five-minute marker and the hourly job can coexist with a command
// that takes longer than either: TestThePurgeKeepsTheClaimOfACommandStillRunning is
// the case where the alternative is the same submission committing twice.
//
// What a lapsed lease licenses is a refusal, and never a second run. A claim that
// outlived its owner says either "it committed and the answer was lost" or "nothing
// committed", and the kernel owns no statement that tells the two apart after the
// fact: the answer is written after the commit, detached, by a request that can fail
// at exactly that step — the store can take the domain row and refuse the response it
// was written beside, which is TestACommandWhoseAnswerCannotBeStoredIsNotAppliedAgainAfterPurge.
// Freeing such a key is therefore a permission to write it twice, which is the one
// promise this table exists to keep. So a claim that lost its owner is spent rather
// than freed: settleUncertainSQL settles it as "its outcome is not held" for the rest
// of the day the key was promised, and every press that arrives afterwards is told to
// read the result. Deleted is a settled answer whose day is over, and a claim older
// than the day it was written for and nobody is answering: past the day a key is a
// fresh command by the document's own words (docs/adr/0016).

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
	"sync"
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

// idempotencyTable is the one this package owns. See migrations/000044_idempotency.
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
	// claimBudget bounds one press of the mechanism's two statements, for the reason
	// kit/limit gives for its own: neither is allowed to turn a database that has
	// stopped answering into a request that never ends. claimAttempts says how many
	// presses that budget buys before the command is refused.
	claimBudget = 2 * time.Second
	// claimLease is how long a claim is believed to have an owner after the last
	// word from it, and claimRenewal is how often an owning request says "still
	// here". Three renewals fit inside a lease, so one statement that found the
	// database slow is not what strips a running command of its fence; and a process
	// that stops answering altogether loses it inside two minutes, which is shorter
	// than the five-minute marker it used to wait for. Both numbers are in the
	// migration's note on owner_lease. What they buy the caller of a command whose
	// owner died is an answer rather than a wait: the lease is the moment the claim
	// becomes provably unanswerable, and the press that arrives after it is told so
	// instead of being told to wait for a scheduled job.
	claimLease   = 2 * time.Minute
	claimRenewal = claimLease / 3
	// claimLeaseSQL is the same length in the one spelling Postgres adds to now().
	// Two spellings of one number is the price of the database owning the clock;
	// TestTheClaimsLeaseIsTheOneThisPackageRenews keeps them from drifting.
	claimLeaseSQL = "2 minutes"
)

// outcomeUncertainSQL is one predicate over the row's own three clocks, and the
// single reading of the state a claim reaches when the request that wrote it can
// no longer answer for it: not settled, past the in-flight marker it claimed with,
// no lease renewed inside a lease, and still inside the day the key was promised
// for. It is asked of the row rather than of the clock, and it is asked once, here,
// by the two steps that act on it — the request that arrives to find it and the
// scheduled job — because the two must not disagree about when a claim is dead.
//
// What it deliberately does not claim is to know whether the command ran. It cannot:
// the answer is written after the commit and outside it, so a store that took the
// domain rows and refused the response leaves exactly this row behind. That is why
// the state below settles a key rather than releasing it.
const outcomeUncertainSQL = "NOT settled AND expires_at < now()" +
	" AND (owner_lease IS NULL OR owner_lease < now())" +
	" AND claimed_at > now() - interval '24 hours'"

// settleUncertainSQL is what such a claim becomes: it ran, or it did not, and the
// answer is gone either way. `response = NULL` is the state an answer too big to
// hold already uses — the row says the command is over and the repeat is told to
// read the result — which is the honest reading of a lost one too, and the same one
// that keeps the key. `expires_at` is re-stated as the day the claim was written for
// rather than left at the five-minute marker, so the row that is now a fence is not a
// candidate for the next purge tick. Nothing in this statement guesses an outcome: no
// status, no body, no replay.
const settleUncertainSQL = "UPDATE " + idempotencyTable +
	" SET settled = true, response = NULL, owner_lease = NULL," +
	" expires_at = claimed_at + interval '24 hours'"

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

// commitVerdict is what the middleware that ended this request's transaction
// learned about it, and what the response it wrote cannot say. Its three states are
// the three answers a caller of a command may be given about the write behind the
// answer, and the record step acts on them differently.
type commitVerdict uint8

const (
	// commitCommitted: the transaction ended as the response says it did. This is the
	// zero value because it is the verdict of every request whose transaction
	// middleware had nothing to complain about — including the handler's own refusal,
	// which rolled the transaction back by asking for nothing to be kept.
	commitCommitted commitVerdict = iota
	// commitRolledBack: the work is not in the database, whatever the bytes the
	// handler wrote say. See noteUncommitted.
	commitRolledBack
	// commitUnacknowledged: a COMMIT was sent and its answer never came back. The
	// work may be in the database or nowhere. See noteCommitUnacknowledged.
	commitUnacknowledged
)

// idempotencyHolder is where a request that owns its claim says so, on the
// context, for the middleware on the way back out. A pointer, because the two
// halves of the mechanism are on opposite sides of the transaction.
type idempotencyHolder struct {
	claim *idempotencyClaim
	// commit is the transaction middleware's verdict, which the response cannot
	// carry. See noteUncommitted for why the record step needs it and cannot get it
	// from the buffered status.
	commit commitVerdict
	// stopLease ends the renewal this request started when it took the claim. A
	// goroutine that outlived the request would keep renewing a lease nobody holds,
	// which is the fence standing after the thing it fences is gone.
	stopLease func()
}

// stop ends this request's renewal, once, and is safe on a holder that never took
// a claim at all.
func (h *idempotencyHolder) stop() {
	if h != nil && h.stopLease != nil {
		h.stopLease()
	}
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
		h.commit = commitRolledBack
	}
}

// noteCommitUnacknowledged tells the record step that this request sent a COMMIT and
// lost its answer, which is the one transaction failure the record step may not read
// as a rollback.
//
// The difference from noteUncommitted is the whole of the difference between a fence
// and a double apply. A rollback is a fact: nothing is in the database, the retry of
// that command must run, and the key that would stop it is deleted. A lost
// acknowledgement is the absence of a fact: the write may have landed, and the only
// statement that could have said so is the one that did not arrive. Freeing the key
// then takes the caller's last protection away — it comes back with the same key to
// ask whether its write landed, and the kernel, which cannot say, runs the write a
// second time. So the claim is settled as the row that admits it holds nothing
// instead: settleUncertainSQL, the same statement that spends a claim whose owner
// stopped answering, and the same one honest reading of "it ran, or it did not, and
// the answer is gone either way". TestALostCommitAcknowledgementDoesNotReleaseTheCommandKey
// is the case, and it fails on the deletion.
func noteCommitUnacknowledged(ctx context.Context) {
	if h := holderFrom(ctx); h != nil {
		h.commit = commitUnacknowledged
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
		a.attachLease(ctx.Context(), holder, c)
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
		holder := &idempotencyHolder{}
		r = r.WithContext(context.WithValue(r.Context(), claimKey{}, holder))
		defer func() {
			// Whatever way this request leaves, its claim stops having an owner here:
			// the renewal below is stopped, and the record step writes a NULL lease so
			// the row reads as "nobody is answering this" rather than as a command that
			// stopped being answered at the moment its deadline happened to fall.
			defer holder.stop()
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
			if err := a.recordIdempotency(r.Context(), *holder.claim, status, b.Header(), body, held, holder.commit); err != nil {
				// The command ran and its answer was not kept. The caller has its
				// response and nobody else ever will: the row stays unsettled, which
				// refuses a repeat while its marker stands and spends the key as "its
				// outcome is not held" once it lapses — see settleUncertainSQL — rather
				// than handing back a claim that would run the write a second time. This
				// statement is not retried, because a record store that is failing is
				// failing for the request too, and the direction that is safe here is the
				// one that keeps the fence rather than the one that keeps the answer: a
				// row that lies about an answer replays one, and a row that admits it holds
				// nothing asks to be read instead.
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
	if err := a.recordIdempotency(ctx, *holder.claim, http.StatusInternalServerError, nil, nil, false, commitRolledBack); err != nil {
		a.rlog(ctx).ErrorContext(ctx, "httpx: the idempotency claim of a panicked command could not be released",
			"error", err)
	}
}

// recordIdempotency is the decision, made as one statement against one table: either
// release the claim, or settle it with the answer.
//
//   - A COMMIT whose answer was lost settles the claim as "its outcome is not held"
//     and keeps the key, whatever the response says and whatever the verdict's name
//     suggests. Nothing may run again under this key, and nothing may be promised
//     from it either: the write may have landed and its answer is not held. This
//     branch is first because it outranks the two below — the 500 the transaction
//     middleware substitutes for a commit it could not read is exactly the status that
//     would otherwise delete the fence. See noteCommitUnacknowledged.
//   - A 5xx, in any form — the handler's own, the 500 the transaction substitutes
//     when the commit was refused, the 500 a panic recovery wrote — deletes the claim.
//     A refusal nobody received is not an answer to remember, and a retry of work
//     that rolled back must run: house rule 9 said backwards.
//   - A transaction that did not commit deletes it too, whatever the buffered
//     response says. This is the same fact as the bullet above, read from the side
//     that cannot answer: a commit that failed on a request whose caller had already
//     gone away leaves a 200 nobody read behind, and `commitRolledBack` is how the record
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
func (a *API) recordIdempotency(ctx context.Context, c idempotencyClaim, status int, header http.Header, body []byte, held bool, commit commitVerdict) error {
	return a.detached(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		if commit == commitUnacknowledged {
			// Spent, not freed, and not answered: the statement is the one this package
			// already uses for a claim whose owner stopped answering, because the row is in
			// the same state for the same reason — the command may have run, and no step on
			// this side of the lost answer can say which. It writes no status, no body and
			// no replay; it writes a fence that holds until the key's own day is over.
			return tx.DB().Exec(settleUncertainSQL+
				" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
				c.tenant, c.actor, c.operation, c.key).Error
		}
		if commit == commitRolledBack || status >= http.StatusInternalServerError {
			return tx.DB().Exec("DELETE FROM "+idempotencyTable+
				" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ?",
				c.tenant, c.actor, c.operation, c.key).Error
		}
		if !held || len(body) > maxStorableResponse {
			return tx.DB().Exec("UPDATE "+idempotencyTable+
				" SET settled = true, status = ?, response = NULL, owner_lease = NULL,"+
				" expires_at = claimed_at + interval '24 hours'"+
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
			" request_id = ?, expires_at = claimed_at + interval '24 hours', owner_lease = NULL"+
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
	// outcomeUncertain is the row's own answer to the question the purge asks, read
	// by the request that arrives first: an unsettled claim past its marker, past its
	// lease, inside its day. One predicate, in outcomeUncertainSQL, asked in both
	// places — see settleUncertainSQL for what the state is turned into and why no
	// statement here claims to know whether the command ran.
	outcomeUncertain bool
}

// claimAttempts is how many times the gate presses for the claim row before it
// refuses the command. One press is what a healthy database costs; the two behind it
// are for the failure this kernel measured on a loaded host: a claim whose single
// INSERT took the whole budget on a Postgres busy copying databases, answered 503
// "the command could not be claimed, and nothing ran" to a person who had pressed a
// deliberate button, when the only thing wrong was that the machine was slow. e2e's
// reload journey was the first thing on this tree to see it, and it saw it because a
// browser is the only witness that asks twice for the same button press.
//
// Pressing again is safe because of what a failed press can and cannot have left
// behind. A statement that failed, or a transaction that never got as far as one, was
// aborted: Postgres keeps nothing from an aborted transaction, so the next press meets
// no row of this request's. The one failure that can leave a row standing is a COMMIT
// nobody read the answer to — and the row it left is a fence this request cannot prove
// it owns: the only thing the row knows about its writer is the request id, which the
// caller can choose. claimPressAgain refuses to press for that one, because taking over
// a claim another request wrote is the double-apply the whole table exists to prevent.
const claimAttempts = 3

// claimIdempotency is the three-way decision, in one transaction of its own: run,
// replay, or refuse. Detached, because an in-flight marker written inside the
// request's own transaction is invisible to the concurrent repeat that exists to
// be refused — and the loser of that race is the whole point of it.
//
// The press is repeated while the failure says nothing was left behind, because the
// alternative is to refuse a command on the strength of one slow statement: see
// claimAttempts. Each press carries its own claimBudget; the total is that budget times
// the presses, and a request never waits longer than the sentence says.
func (a *API) claimIdempotency(ctx context.Context, c idempotencyClaim) (idempotencyAnswer, error) {
	for attempt := 1; ; attempt++ {
		out, err := a.claimOnce(ctx, c)
		if err == nil {
			return out, nil
		}
		if attempt >= claimAttempts || !claimPressAgain(err) {
			return idempotencyAnswer{}, err
		}
		a.rlog(ctx).WarnContext(ctx, "httpx: the idempotency claim failed where a press again is safe",
			"attempt", attempt, "method", c.operation, "key", c.key, "error", err)
	}
}

// claimPressAgain says whether a claim that failed may be attempted again. It refuses
// the one failure that could have committed a row — db.ErrCommitUnacknowledged, whose
// whole meaning is that the answer was never read (kit/db/tx.go's outcomeKnown keeps
// the two failures that do state an outcome out of it) — and takes the two shapes of
// failure that cannot have committed anything this request could mistake for its own.
//
// The first is this request's own budget running out: the transaction it bounded was
// rolled back the moment the budget went off, so there is nothing to find. The second
// is the database answering the statement with a class that says "not now": 40001 and
// 40P01 where two writers chose badly, 55P03 where a wait ran out, 08000, 08003 and
// 08006 where the connection did, 53300 where the slot ran out, 57P01, 57P02 and 57P03
// where the server itself did. Every class the database answers with and this does not
// name — 42P01 for a table that is not there, 42501 for a policy that refuses — is the
// same answer whenever it is asked, and pressing three times would only delay the
// refusal by six seconds. A failure with no SQLSTATE and no deadline in it is unknown,
// and the unknown is refused rather than pressed.
func claimPressAgain(err error) bool {
	if errors.Is(err, db.ErrCommitUnacknowledged) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) {
		return false
	}
	switch sqlState.SQLState() {
	case "40001", "40P01", "55P03", "08000", "08003", "08006", "53300", "57P01", "57P02", "57P03":
		return true
	}
	return false
}

func (a *API) claimOnce(ctx context.Context, c idempotencyClaim) (idempotencyAnswer, error) {
	var out idempotencyAnswer
	err := a.detached(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		for range 2 {
			// Twice, because the row can stop existing between the two statements: the
			// claim of a request that failed is deleted, an expired answer is deleted
			// below, and a purge can take either between the failed INSERT and the read.
			// Neither is a fault of this request's, and neither may be answered with a
			// refusal of an in-flight command that is not in flight.
			res := tx.DB().Exec("INSERT INTO "+idempotencyTable+
				" (tenant_id, actor_id, operation, key, request_hash, claimed_at, expires_at, owner_lease)"+
				" VALUES (?, ?, ?, ?, ?, now(), now() + interval '5 minutes', now() + ?::interval)"+
				" ON CONFLICT (tenant_id, actor_id, operation, key) DO NOTHING",
				c.tenant, c.actor, c.operation, c.key, c.hash[:], claimLeaseSQL)
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
				// see the note on expires_at below, and the uncertain branch under this one.
				if err := tx.DB().Exec("DELETE FROM "+idempotencyTable+
					" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ? AND settled",
					c.tenant, c.actor, c.operation, c.key).Error; err != nil {
					return err
				}
				continue
			}
			if h.outcomeUncertain {
				// Nobody can answer this claim any longer and its day is not over, which
				// leaves two readings and no statement that chooses between them: the command
				// committed and the response store refused its record, or the process died
				// before it wrote anything. The claim is the fence against exactly that
				// uncertainty, so the press that arrives is the step that writes the verdict
				// into the row: the row becomes the answer "its outcome is not held" and keeps
				// the key for the rest of its day. Freeing it here would be the kernel deciding
				// on nothing that a write it cannot see did not happen.
				//
				// The predicate is repeated in the statement rather than trusted from the read
				// above, because the owner can answer between the two: if it did, no row
				// matches, nothing was settled by this request, and the answer it wrote stands.
				if err := tx.DB().Exec(settleUncertainSQL+" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ? AND "+
					outcomeUncertainSQL,
					c.tenant, c.actor, c.operation, c.key).Error; err != nil {
					return err
				}
				out = outcomeUncertain()
				return nil
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
		return outcomeUncertain()
	default:
		return idempotencyAnswer{held: h}
	}
}

// outcomeUncertain is the refusal of a command whose answer no longer exists: not
// "somebody is running it", and not correctable by waiting, because the outcome is a
// fact somebody has to read out of the list rather than one another write under the
// same key can settle. Two routes reach it — an answer too big or already on the wire,
// which the kernel knows ran, and a claim whose owner can no longer be asked, which it
// does not know — and the sentence says no more than both of them support.
func outcomeUncertain() idempotencyAnswer {
	return idempotencyAnswer{refuse: http.StatusConflict,
		code: CodeIdempotencyResponseNotHeld + ": this command may already have run; its answer is not held; read the result rather than sending it again"}
}

// inProgress is the refusal of a command somebody else is running: correctable by
// waiting and by nothing else, and never by rewriting the other request's outcome.
func inProgress() idempotencyAnswer {
	return idempotencyAnswer{refuse: http.StatusConflict, code: CodeIdempotencyInProgress + ": another request with this key is still running", retryAfter: 2}
}

// reclaimSQL is gone, and its absence is the point: no statement in this package
// takes over another request's claim. What a claim whose owner died recovers is a
// refusal, and the settle that writes it is asked for by whichever step reaches the row
// first — the arriving request or the scheduled job — on the one condition the row's own
// lease can carry, that nobody is renewing it. That is what lets the same evidence be the
// recovery and the fence: it ends nobody's command, and it frees nobody's key.

// attachLease is the owning request saying "still here", for as long as it is
// here.
//
// The claim it wrote is what refuses a repeat, and the lease is what makes that
// refusal true of a command rather than of a clock: the five-minute marker in the
// row is a bound on how long a key can be held by a process that stopped
// answering, not a statement that a command which has run for five minutes has
// finished. Renewal every third of the lease means a claim survives one statement
// that found the database slow and dies within two minutes of the process that
// held it — so a claim can be settled on evidence instead of on elapsed age, which is
// the difference between spending a dead key and stealing a live one.
// TestThePurgeKeepsTheClaimOfACommandStillRunning is the case the fence is for;
// TestThePurgeSpendsTheClaimOfACommandWhoseLeaseLapsed is the case the evidence is for.
//
// A renewal that fails is logged and retried on the next tick rather than
// abandoned: giving up on the first failure is what would put a slow command back
// in the world of the clock, and a request that has stopped is stopped by its own
// context and by stop, not by a failed statement.
//
// The two ways it stops fence differently, and the weaker of them is still the
// invariant the settle needs. `stop` runs in the record step's defer, so a claim
// never outlives the request that answered it. The other is the caller hanging up,
// and a request whose context ended cannot commit any more: database/sql has
// already rolled its transaction back by the time this notices, which is the fact
// `hungUp` is written about. So the fence is not standing over work that can still
// land, and what a lapsed lease is ever really telling the next request is that no
// claim which can still write a row is spent as though nobody owned it.
func (a *API) attachLease(ctx context.Context, holder *idempotencyHolder, c idempotencyClaim) {
	gone := make(chan struct{})
	once := sync.OnceFunc(func() { close(gone) })
	holder.stopLease = once
	go func() {
		ticker := time.NewTicker(claimRenewal)
		defer ticker.Stop()
		for {
			select {
			case <-gone:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := a.renewLease(ctx, c); err != nil {
					a.rlog(ctx).ErrorContext(ctx, "httpx: the idempotency claim's lease could not be renewed",
						"method", c.operation, "key", c.key, "error", err)
				}
			}
		}
	}()
}

// renewLease pushes the row's belief about its owner out another lease. It is
// keyed on all four primary-key columns for the reason the record step is: a
// statement that could reach another caller's row with the same key would renew
// somebody else's fence. A row that is already settled was answered by this very
// request and is not being answered any longer, so `NOT settled` keeps a renewal
// from putting a lease back on a row the record step just cleared one off.
func (a *API) renewLease(ctx context.Context, c idempotencyClaim) error {
	return a.detached(ctx, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("UPDATE "+idempotencyTable+" SET owner_lease = now() + ?::interval"+
			" WHERE tenant_id = ? AND actor_id = ? AND operation = ? AND key = ? AND NOT settled",
			claimLeaseSQL, c.tenant, c.actor, c.operation, c.key).Error
	})
}

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
// The last two columns are read on the database clock like everything else here.
// What the row's deadline means depends on the row's state, and the two readings
// must not be confused: on a settled row it is the end of the window the answer is
// kept for, which this request enforces for itself; on an unsettled one it is the
// marker past which the claim is asked, by its own lease, whether anybody is still
// answering it. A request may act on that answer — the uncertain branch in
// claimIdempotency asks the purge's own question, from the one predicate in
// outcomeUncertainSQL, so that the two steps cannot disagree about when a claim is
// dead — but it may only act on it by refusing and settling the row as unanswered,
// which is the one action that costs a command nothing whichever way the truth lies.
// What no step may do on this evidence is delete the row.
func readHeld(tx db.Tx[db.System], c idempotencyClaim) (*heldResponse, error) {
	rows, err := tx.DB().Raw("SELECT settled, status, content_type, headers, response, response IS NOT NULL, request_hash, request_id,"+
		" now() > expires_at, ("+outcomeUncertainSQL+") FROM "+idempotencyTable+
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
	if err := rows.Scan(&h.settled, &h.status, &h.contentType, &headers, &h.body, &h.holds, &h.hash, &h.requestID, &h.expired,
		&h.outcomeUncertain); err != nil {
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

// PurgeIdempotency empties the table without ever emptying a fence. Two statements
// delete, and one of them writes: the answers whose day is over, and the claims
// older than the day they were written for that nobody is answering any longer, go;
// a claim that lost its owner inside its day is settled as "its outcome is not held"
// and stays. As with limit.Purge this is a scheduled job and never a side effect of a
// claim: a request that also deleted somebody else's row is a request paying for
// traffic it is not refusing, and a table nobody empties grows at somebody else's
// rate. The expires_at index above is the first statement's query.
//
// The settle is the whole of the difference between this job and the one it replaced.
// An unsettled row past its marker is one of two commands — one that is still being
// answered and one whose process is gone — and the row knows which, because the
// request that owns it renews its lease: a lapsed lease is the fact of a dead owner,
// and a live one is not a candidate at any age. Deleting on expires_at alone deleted
// the claim of a command that was simply slow, and the submission that repeated it
// committed a second row and a second event. But a lapsed lease only ever proves that
// nobody can answer the claim; it never proves the command did not commit, and the
// record that would have said so is written after the commit and can itself fail.
// Deleting such a row hands the key back to the caller that is asking whether its
// write landed, which is the double apply this table exists to make impossible. The
// row is therefore kept and marked uncertain, and the key it holds is spent until its
// own day is over. The cost is one press and a look at the list; the alternative is a
// row nobody can explain and an effect nobody asked for twice.
func PurgeIdempotency(ctx context.Context, conn *db.Conn) error {
	return db.RunSystem(ctx, conn, idempotencyToken, func(_ context.Context, tx db.Tx[db.System]) error {
		// Spent, not freed — the predicate is the purge's own, unqualified, because
		// this is the whole table asked at once. A row whose lease is current is not
		// eligible whatever its deadlines, which is why a command slower than the
		// marker and slower than this job keeps its fence.
		if err := tx.DB().Exec(settleUncertainSQL + " WHERE " + outcomeUncertainSQL).Error; err != nil {
			return errors.New("httpx: purge idempotency: " + err.Error())
		}
		// The first statement is the promise's end: an answer kept for a day is deleted
		// when the day is over. The second is its twin for a claim that was never
		// answered at all — the row has nothing left to promise once its day has passed,
		// and emptying it is what keeps the table a table somebody can explain — and it
		// carries the lease guard the settle has because a command that has been running
		// for a day is a command still being answered.
		for _, delete := range []string{
			"DELETE FROM " + idempotencyTable + " WHERE expires_at < now() AND settled",
			"DELETE FROM " + idempotencyTable + " WHERE claimed_at < now() - interval '24 hours'" +
				" AND (owner_lease IS NULL OR owner_lease < now())",
		} {
			if err := tx.DB().Exec(delete).Error; err != nil {
				return errors.New("httpx: purge idempotency: " + err.Error())
			}
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
//
// The replacement goes on two requests, not one. The struct this middleware was
// handed is the one huma decodes from, and `bodies` says why it is not the one a
// handler reads: huma copies the request on every WithContext, and carry put the
// pre-routing struct on the context, which is what RequestFrom answers with and
// what a route that reads its own form reads. Both structs name the same reader —
// a copied Request copies the Body field, not the stream behind it — so reading
// here drains the reader the handler is about to read, and a replacement written
// only on this copy leaves that one returning nothing at all. A premise field the
// kernel silently swallows is worse than a body that fails to arrive: the module
// below it reads an empty form, finds no premise to check and writes anyway.
// TestAKeyedRevocationStillChecksTheSessionsRevision is the case, and it is a
// page-route case because a page route is the kind that reads its own body.
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
		// to get, from the same reader. Both structs again — see the note above.
		rest := io.NopCloser(io.MultiReader(bytes.NewReader(body), limited))
		r.Body = rest
		if carried, ok := RequestFrom(ctx.Context()); ok && carried != nil && carried != r {
			carried.Body = rest
		}
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if carried, ok := RequestFrom(ctx.Context()); ok && carried != nil && carried != r {
		carried.Body = io.NopCloser(bytes.NewReader(body))
	}
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
