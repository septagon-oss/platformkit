package contracts

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/limit"
)

// The lockout: ten failures for one address in fifteen minutes, and that
// address stops being tried until the window passes.
//
// Ten is enough that a person who has forgotten which of their passwords this
// is does not lock themselves out, and few enough that an online guessing
// attack is not worth running. Fifteen minutes is short enough that the person
// who did lock themselves out can have a coffee.
const (
	MaxAttempts   = 10
	AttemptWindow = 15 * time.Minute

	// MaxSources is how many distinct addresses may fail against one account
	// inside a window before the lockout stops being a lockout.
	//
	// The lockout above is a denial of service somebody else can trigger. Ten
	// wrong passwords for ada@acme.example.com locks Ada out, and an attacker
	// who wants Ada locked out needs nothing but her address — from a botnet,
	// once every fifteen minutes, for as long as they care to. The failures
	// coming from many addresses is what tells the two apart: a person
	// mistyping their password is one machine, and a distributed lockout attack
	// is not. Past this many sources the account gets a delay instead of a
	// refusal, so the attack costs the attacker time and costs Ada nothing but
	// two seconds when she gets it right.
	//
	// Three, because a person legitimately fails from a laptop, a phone and an
	// office network, and rarely from a fourth inside a quarter of an hour.
	MaxSources = 3

	// SoftDelay is what an account under a distributed attack gets instead of a
	// refusal. Two seconds is nothing to a person typing a password and is the
	// difference between an online guessing attack running at the speed of the
	// network and running at half an attempt a second per connection.
	SoftDelay = 2 * time.Second

	// SourceAttempts is the per-address limit, which is the half the per-account
	// one cannot cover: one machine working through a list of addresses fails
	// once against each and never trips an account's counter. It is higher than
	// MaxAttempts because one address is legitimately many people — an office,
	// a mobile carrier's NAT — and the number is what an attacker gets rather
	// than what anybody needs.
	SourceAttempts = 50

	// ResetRequests is how many forgotten-password links one address may ask
	// for inside a window.
	//
	// The route it guards is public, answers the same either way, and costs a
	// mail: without a cap, twenty-three requests were twenty-three mails to
	// somebody who asked for none, which is a mail relay's reputation spent by
	// a stranger with a browser. It counts the address asking and never the
	// address asked about, so the cap cannot be used to tell an account that
	// exists from one that does not — that is the whole property the route is
	// built around.
	//
	// Ten, because a person who has lost a password asks two or three times and
	// an office behind one NAT is several people doing that at once.
	ResetRequests = 10
)

// Limiter counts failed logins, per account and per source address, in the one
// counter every replica shares (kit/limit).
//
// Two counters, because one of them is a weapon. A per-account lockout is a
// denial of service anybody can trigger against anybody whose address they
// know, and the fix is not to remove it — a guessing attack against one account
// is the common case — but to tell the two apart. Failures against one account
// from many addresses are somebody attacking the lockout, and that account gets
// a delay rather than a refusal; failures from one address against many
// accounts are somebody working through a list, and that address gets the same.
//
// # Counting distinct addresses without a set
//
// MaxSources asks how many places one account has failed from, and the store
// counts events rather than remembering them. So the first failure of an
// account-and-address pair inside a window — a limit of one on the pair's own
// key — is what raises the account's source count. A hundred failures from one
// address raise it once, which is the question being asked.
//
// # The residual, stated
//
// Only the distributed lockout is defused. Ten wrong passwords for
// ada@acme.example.com from one address still lock Ada out for fifteen minutes,
// and an attacker who wants exactly that needs nothing but her address and one
// machine. MaxSources does not help: one source is not many. That is accepted,
// not overlooked, and it is accepted because the alternative is worse — an
// account with no lockout is an account a botnet guesses at the speed of the
// network, and the common attack is guessing rather than griefing. What is
// bought back is the scale of it: the attacker has to spend fifteen minutes per
// account per window, they are recorded in auth.login_failed with Locked set
// while they do it, and the person they are locking out is told why by
// ErrTooManyAttempts rather than left thinking their password stopped working.
// Closing it properly means proving the caller is not the account's owner —
// a second factor, or a challenge — and neither exists here yet.
//
// A limiter that cannot reach its store allows the attempt and says so in the
// log. It is the right way for this one to fail: the alternative is an
// installation nobody can sign in to because a counter table is unreachable,
// and every refusal this makes is a refusal the password check would make
// anyway.
//
// It lives in contracts rather than in internal/ because the lockout is part of
// what Login promises, so the fake keeps it too and the conformance suite can
// check that both do.
type Limiter struct{ store limit.Limiter }

// NewLimiter returns a limiter over a store: kit/limit's Postgres one in the
// application, its memory one in a fake.
func NewLimiter(store limit.Limiter) *Limiter { return &Limiter{store: store} }

// The keys, each namespaced by what it counts. kit/limit puts the tenant in
// front of them, so these say nothing about which customer is being attacked.
// The separator inside a compound key is a space, which no address contains.
func accountKey(email string) string   { return "auth/account/" + EmailKey(email) }
func sourcesKey(email string) string   { return "auth/sources/" + EmailKey(email) }
func sourceKey(ip string) string       { return "auth/source/" + ip }
func pairKey(email, ip string) string  { return "auth/pair/" + EmailKey(email) + " " + ip }
func forgotKey(ip string) string       { return "auth/forgot/" + ip }
func notedKey(email, ip string) string { return "auth/noted/" + EmailKey(email) + " " + ip }

// Verdict is what the limiter says about one attempt before it is made.
type Verdict int

const (
	// Allow: try the password.
	Allow Verdict = iota
	// Delay: try the password, after SoftDelay.
	//
	// It is the answer to an account whose failures came from many addresses,
	// and to an address that has failed against many accounts. Neither can be
	// refused outright: refusing the first is the lockout an attacker triggers
	// on somebody else's behalf, and refusing the second locks out an office
	// behind one NAT. A delay costs the person one pause and costs an attacker
	// the whole rate of their attack.
	Delay
	// Refuse: too many failures for this one account from one or two places,
	// which is somebody guessing a password rather than somebody attacking the
	// lockout. The caller gets ErrTooManyAttempts.
	Refuse
)

// Check is what the limiter says about an attempt on this address from this
// one. It reads and does not record; Failed and Succeeded are the writes.
func (l *Limiter) Check(ctx context.Context, email, ip string) Verdict {
	if l.count(ctx, accountKey(email)) >= MaxAttempts {
		if l.count(ctx, sourcesKey(email)) > MaxSources {
			// Locked, but by a crowd: this is somebody trying to lock the
			// account's owner out, and answering "too many attempts" is doing
			// it for them.
			return Delay
		}
		return Refuse
	}
	if ip != "" && l.count(ctx, sourceKey(ip)) >= SourceAttempts {
		// One machine working through a list of addresses. Each account's own
		// counter is untouched, so this is the only thing that sees it.
		return Delay
	}
	return Allow
}

// Failed records one failure, from one address, against one account.
func (l *Limiter) Failed(ctx context.Context, email, ip string) {
	l.record(ctx, accountKey(email), MaxAttempts)
	if ip == "" {
		return
	}
	// The first failure of this pair in this window, and only it, is a new
	// place this account has been attacked from.
	if l.within(ctx, pairKey(email, ip), 1) {
		l.record(ctx, sourcesKey(email), MaxSources)
	}
	l.record(ctx, sourceKey(ip), SourceAttempts)
}

// Succeeded forgets an account's failures: somebody proved they are who they
// said. The addresses stay — one correct password out of fifty attempts is what
// a successful guessing run looks like — and so does the count of places this
// account has been attacked from, because a sign-in is not evidence that the
// attack stopped. The consequence is the safe one: a lockout that follows is a
// delay rather than a refusal.
func (l *Limiter) Succeeded(ctx context.Context, email string) {
	if err := l.store.Forget(ctx, accountKey(email)); err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not forget an account", "error", err)
	}
}

// Requested counts one forgotten-password request from an address and reports
// whether it is within ResetRequests for this window. The address is the one
// asking; the address being asked about is never counted, because a cap that
// depended on it would answer the question the route exists not to answer.
func (l *Limiter) Requested(ctx context.Context, ip string) bool {
	return l.within(ctx, forgotKey(ip), ResetRequests)
}

// Noted reports whether a refusal for this account from this address is worth
// writing down, which is the first one in the window and no more.
//
// A locked-out account under a script is refused before anything is checked,
// and recording each refusal was two audit rows per attempt for a fact that
// does not change: the first one says the account is under attack and the
// nine hundredth says it still is. The failures themselves are still recorded
// one for one — those are attempts that were actually made.
func (l *Limiter) Noted(ctx context.Context, email, ip string) bool {
	return l.within(ctx, notedKey(email, ip), 1)
}

// count is how many events a key has in the window, or zero when the store
// cannot be reached: a limiter that refused what it could not count would make
// an unreachable table an installation nobody can sign in to.
func (l *Limiter) count(ctx context.Context, key string) int {
	n, _, err := l.store.Count(ctx, key, AttemptWindow)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not be read; the attempt is allowed", "key", key, "error", err)
		return 0
	}
	return n
}

// within records one event and reports whether the key is still inside max.
func (l *Limiter) within(ctx context.Context, key string, max int) bool {
	ok, _, err := l.store.Allow(ctx, key, max, AttemptWindow)
	if err != nil {
		slog.ErrorContext(ctx, "auth: the limiter could not be written; the attempt is allowed", "key", key, "error", err)
		return true
	}
	return ok
}

// record counts an event whose verdict is read separately, because the lockout
// has three answers rather than two: what a failure is worth is Check's to say,
// after the count that Failed has just raised.
func (l *Limiter) record(ctx context.Context, key string, max int) { _ = l.within(ctx, key, max) }

// EmailKey is the address as this module counts it: one that differs only in
// case or in whitespace is the same account being attacked, and the same
// account in an audit trail. It is exported because the event that records a
// failure has to spell the address the same way the limiter counted it.
func EmailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
