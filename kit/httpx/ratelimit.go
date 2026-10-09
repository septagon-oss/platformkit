package httpx

// ratelimit.go puts a limit's own numbers on the answer it decided.
//
// A public API states its limits on the wire so a client can back off before it is
// refused rather than after (draft-ietf-httpapi-ratelimit-headers). kit/limit has
// counted the anonymous writes of the Public surface and the asks of the access door
// for a while, and said nothing beyond one `Retry-After` on one of the two refusals —
// which leaves a well-behaved client guessing, and a guess it gets wrong is traffic
// this server paid for and threw away.
//
// The one rule here is that every number on these fields came from the counter that
// decided the answer. `kit/limit` decides and reads in one statement: `Reserve` records
// the event and returns, from that one statement, the decision, the quota left and the
// window left, so the policy line costs no statement at all — a name, a quota and a
// window the guard was built with — and the rate line is the reading that decided. The
// shape before it was `Allow` and a second `Count` taken after the refusal: one SELECT
// against a row the first had already written, stating a quota taken from a row another
// request had moved since the attempt being answered. `Count` stays for the read that
// has to come before an attempt — modules/auth's lockout asks how full a window is and
// must not fill it — and is not how a response states itself. Adding `Reserve` to an
// interface published at v1.1.0 broke whoever implemented it, and that is a decision the
// diff of scripts/baselines records, not one a middleware makes in passing.

import (
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// The three fields, spelled once: here, at the writers, and in the README.
const (
	RateLimitHeader       = "RateLimit"
	RateLimitPolicyHeader = "RateLimit-Policy"
	RetryAfterHeader      = "Retry-After"
)

// defaultRetryAfter is the wait a 429 names when the writer of it named none, and
// the 429 rule of this package: every refusal this package decides already names its
// window, and the ones that do not came from a handler counting somewhere kit/httpx
// cannot see (modules/auth's lockout and its mail caps answer 429 from their own
// counters, which do know the wait, and stating it there is that module's change to
// make). Until it does, the kernel refuses to answer "too many" with no number at
// all, and says which route did — a guarantee that holds for every 429 is worth more
// than a guarantee that holds for the two guards this package happens to own.
//
// The rule is that a refusal which says "too many" and no number tells a client
// nothing it can act on, and every client then retries at its own guess — which is
// the traffic this response exists to prevent. It is a floor and not a reading of any
// counter: the routes that decide a refusal from a real window write that window here
// instead, and only a handler that counted somewhere this package cannot see reaches
// the default. One minute is the shortest window any limit in this kernel runs
// (publicWriteWindow), so it is never a longer wait than the caller is under.
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

// measured is one policy as a counter reported it on this answer.
type measured struct {
	policy    limitPolicy
	remaining int
	left      time.Duration
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
func sayAnswer(ctx huma.Context, got ...measured) {
	if len(got) == 0 {
		return
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
