package httpx

// ratelimit.go puts a limit's own numbers on the answer it decided: a public API states its
// limits on the wire so a client can back off before it is refused rather than after
// (draft-ietf-httpapi-ratelimit-headers), and a client left guessing sends traffic this
// server paid for and threw away.
//
// The one rule here is that every number on these fields came from the counter that decided
// the answer. kit/limit decides and reads in one statement — Reserve records the event and
// returns the decision, the quota left and the window left — so the policy line is a name, a
// quota and a window the guard was built with, and the rate line is the reading that decided.
// Count stays for the read that must come before an attempt: modules/auth's lockout asks how
// full a window is and must not fill it.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/idempotency"
)

// The three fields, spelled once: here, at the writers, and in the README.
const (
	RateLimitHeader       = "RateLimit"
	RateLimitPolicyHeader = "RateLimit-Policy"
	RetryAfterHeader      = "Retry-After"
)

// defaultRetryAfter is the wait a 429 names when the writer of it named none, and the 429
// rule of this package: every refusal this package decides already names its window, and the
// ones that do not came from a handler counting somewhere kit/httpx cannot see and naming no
// wait either. The kernel refuses to answer "too many" with no number at all, and says which
// route did — a guarantee that holds for every 429 is worth more than one that holds for the
// two guards this package happens to own. A module that counts for itself names its own
// window instead (SayCounted is how modules/auth's lockout does it).
//
// It is a floor and not a reading of any counter, and one minute is the shortest window any
// limit in this kernel runs, so it is never a longer wait than the caller is under.
const defaultRetryAfter = 60 * time.Second

// limitPolicy is one policy as the wire names it: a name, a quota, a window. The
// name is the point — draft-ietf-httpapi-ratelimit-headers §3 makes
// `RateLimit-Policy` the field that "SHOULD remain consistent over a sequence of HTTP
// responses", so a client can tell which limit it is under across a whole session
// while the numbers move underneath it.
type limitPolicy struct {
	name   string
	quota  int
	window time.Duration
}

// item is this policy in `RateLimit-Policy`: a quoted name with the quota and the
// window it is measured over. Both are integer seconds, rounded up, because the draft
// says sub-second precision is not supported and a window rounded down is a window the
// client will mis-time.
func (p limitPolicy) item() string {
	return `"` + p.name + `";q=` + strconv.Itoa(p.quota) + `;w=` + secondsUp(p.window, 1)
}

// answer is this policy in `RateLimit`: the quota left under it and what is left of
// the window. `r` is the number a client decides its next request on and `t` is the
// one it decides when to send it, and both are numbers the counter reported rather
// than arithmetic on a count this package does not hold.
func (p limitPolicy) answer(remaining int, left time.Duration) string {
	return `"` + p.name + `";r=` + strconv.Itoa(max(remaining, 0)) + `;t=` + secondsUp(left, 0)
}

// policyField renders `RateLimit-Policy` over every policy that applied to this
// answer, in the order the guards consult them.
func policyField(policies ...limitPolicy) string {
	items := make([]string, 0, len(policies))
	for _, p := range policies {
		items = append(items, p.item())
	}
	return joinList(items)
}

// measured is one policy as a counter reported it on this answer, and the instant the
// answer took the report down. The instant is part of the reading: `left` is a countdown
// from the look at the row, so the window this answer was decided under closes at
// `seen+left` and at no other moment. Measured against the instant the answer was finally
// written instead, the window would outlast its own counter by however long the handler
// spent after its decision, and every retry of that refusal would be told to sit out
// seconds no counter ever opened.
type measured struct {
	policy    limitPolicy
	remaining int
	left      time.Duration
	seen      time.Time
}

func answerField(got ...measured) string {
	items := make([]string, 0, len(got))
	for _, m := range got {
		items = append(items, m.policy.answer(m.remaining, m.left))
	}
	return joinList(items)
}

// joinList is a Structured Fields list: comma-separated, with no space of its own,
// which is all a List[SF] of items with no parameters is.
func joinList(items []string) string { return strings.Join(items, ",") }

// sayPolicy writes `RateLimit-Policy` onto a response held in a huma context: the
// limits that apply to this answer, named, with the quota and window each is measured
// over. It costs no statement to produce, and it is the field the draft asks to stay
// consistent across a sequence of responses, which is what lets a client plan against
// a ceiling it has not reached yet.
func sayPolicy(ctx huma.Context, policies ...limitPolicy) {
	ctx.SetHeader(RateLimitPolicyHeader, policyField(policies...))
}

// sayAnswer writes `RateLimit` from what a counter reported on this answer, and writes
// nothing when no reading came back: a rate line carrying a number this package made
// up is worse than no rate line.
//
// The reading is kept on the held response as well as written to it, because a keyed
// write is answered again from the record and not from this call stack: see sayRecorded.
func sayAnswer(ctx huma.Context, got ...measured) {
	if len(got) == 0 {
		return
	}
	if b, held := bufferFrom(ctx.Context()); held {
		noteCounted(b, got...)
	}
	ctx.SetHeader(RateLimitHeader, answerField(got...))
}

// sayRetryAfter names the wait on a refusal that carries one. It is the same ceiling
// as `t`, with a floor of a second: rounding a 4.2-second wait down to 4 is a client
// that retries 0.2 s early and is refused again, which is the one outcome a
// Retry-After exists to prevent.
func sayRetryAfter(ctx huma.Context, left time.Duration) {
	ctx.SetHeader(RetryAfterHeader, secondsUp(left, 1))
}

// Counted is one counter's decision on the answer a handler is writing, with the
// numbers the statement that decided it reported. It is the same three facts
// publicWrites and the access door already put on the wire — a named quota, what is
// left under it, what is left of the window — for the limits kit/httpx cannot see,
// which are the ones a module counts for itself (modules/auth's caps on mailed
// links and their redemption).
//
// Every field comes from the counter that decided, and that is the rule rather than a
// formality: a policy name and quota a handler copied from its own source is a limit
// the caller has no reason to believe, and a `RateLimit` read out of the row after
// another request has moved it is the same guess. A handler that could not reach its
// counter states nothing: it passes no Counted at all.
type Counted struct {
	// Name is how `RateLimit-Policy` names this counter, and is the counter's own,
	// not the route's: the same cap spent by three routes is one policy on the wire.
	Name      string
	Quota     int
	Window    time.Duration
	Remaining int
	Left      time.Duration
	// Refused says the counter ended this answer. It decides only whether a
	// `Retry-After` goes on: the two rate fields go on either way, because a client
	// that hears the number only when it is refused has already spent the traffic the
	// fields exist to save.
	Refused bool
}

// SayCounted puts the counter that decided this answer on the answer itself.
//
// It is a request-scoped thing, so it says nothing at all to a caller standing outside a
// request — a worker, a test driving a service directly — because there is no answer here to
// state a limit on, and a handler that spends a counter before it is in a request gets the
// decision and no fields.
//
// Both fields are lists, and both are appended to rather than replaced: a public route in an
// application that runs the anonymous-write limit is answered under two counters, and a
// caller told one of them has been told a limit it is not under. The order is the order they
// were consulted, which is what `RateLimit-Policy` asks for.
//
// The reading is written down beside the response, and not only rendered on it, so a keyed
// write that is replayed later can state this counter again (see sayRecorded).
func SayCounted(ctx context.Context, c Counted) {
	b, held := bufferFrom(ctx)
	if !held || c.Name == "" {
		return
	}
	m := measured{
		policy:    limitPolicy{name: c.Name, quota: c.Quota, window: c.Window},
		remaining: c.Remaining,
		left:      c.Left,
	}
	noteCounted(b, m)
	sayCountedOnto(b.Header(), m, c.Refused)
}

// sayCountedOnto writes one counter's numbers onto a response's headers: the ceiling
// it is under, and the reading the counter gave this answer. It is the rendering half of
// the live doors — SayCounted's, publicWrites', and the access door's — and its list is
// appended to rather than replaced for the reason SayCounted spells.
func sayCountedOnto(h http.Header, m measured, refused bool) {
	h.Set(RateLimitPolicyHeader, appendItem(h.Get(RateLimitPolicyHeader), m.policy.item()))
	h.Set(RateLimitHeader, appendItem(h.Get(RateLimitHeader), m.policy.answer(m.remaining, m.left)))
	if refused {
		h.Set(RetryAfterHeader, secondsUp(m.left, 1))
	}
}

// recorded is the answer's own account of the ceilings it was decided under: the same
// readings, in the same order, with each window's closing worked out from the instant
// that reading was taken. This is what a replay restates, and the reason the numbers are
// kept rather than the rendered lines.
func recorded(got []measured) []idempotency.Limit {
	if len(got) == 0 {
		return nil
	}
	out := make([]idempotency.Limit, 0, len(got))
	for _, m := range got {
		out = append(out, idempotency.Limit{
			Name: m.policy.name, Quota: m.policy.quota, Remaining: m.remaining,
			Window: m.policy.window, ResetsAt: m.seen.Add(m.left),
		})
	}
	return out
}

// sayRecorded restates the ceilings a remembered answer was decided under on the reply to
// the attempt that is asking again.
//
// The policy line is not rebuilt here: the kept answer carries it verbatim, which is the only
// complete account of it, and a door names every ceiling it consulted rather than only the
// one that refused. The readings are the harder half, and are stored rather than rendered,
// because what is left of a quota and of a window both shrink while a record sits in one: t
// and Retry-After come from the window's own ending, and the quota is restated as the counter
// reported it. The line is written whole, over whatever a guard above left under that name.
//
// A refusal names the longest of the waits still running over the counters that were shut
// when it was decided; README.md, "A write that is sent twice" holds why, and the case of
// the counter that is still open and so owes no wait.
func sayRecorded(h http.Header, limits []idempotency.Limit, refused bool, now time.Time) {
	if len(limits) == 0 {
		return
	}
	got := make([]measured, 0, len(limits))
	left, spent := time.Duration(0), false
	for _, l := range limits {
		// What is left of the window is worked out again; what was left of the quota is
		// restated. A replay consults no counter — consulting one would be spending the
		// attempt over again — so the quota line can only be the reading the first
		// attempt was given, and `t` is the field that says how long that reading held.
		wait := max(l.ResetsAt.Sub(now), 0)
		got = append(got, measured{
			policy:    limitPolicy{name: l.Name, quota: l.Quota, window: l.Window},
			remaining: l.Remaining,
			left:      wait,
		})
		// A spent quota is what shut an answer. kit/limit answers zero remaining from the
		// statement that refused, in both of its stores, so a reading that reports quota
		// left describes a ceiling the caller was still inside.
		if l.Remaining <= 0 {
			left, spent = max(left, wait), true
		}
	}
	h.Set(RateLimitHeader, answerField(got...))
	if refused && spent {
		h.Set(RetryAfterHeader, secondsUp(left, 1))
	}
}

// appendItem adds one item to a Structured Fields list already on the response, and
// is the whole of the difference between naming a second counter and overwriting the
// first.
func appendItem(list, item string) string {
	if list == "" {
		return item
	}
	return joinList([]string{list, item})
}

// secondsUp is a span in whole seconds, rounded up and not below the floor. A zero is
// allowed for the `t` of a window that closed as we looked at it; a `Retry-After` of
// zero would be an invitation to retry right now.
func secondsUp(d time.Duration, floor int64) string {
	n := int64((d + time.Second - 1) / time.Second)
	if d <= 0 {
		n = 0
	}
	return strconv.FormatInt(max(n, floor), 10)
}
