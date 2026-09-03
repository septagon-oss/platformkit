package contracts

import (
	"strings"
	"sync"
	"time"
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

// Limiter counts failed logins, per account and per source address, in this
// process's memory.
//
// In this process's memory, and that is the honest limit of it: with three
// replicas an attacker gets thirty attempts per window rather than ten, and a
// deploy resets the count. A cluster-wide limit needs a durable shared counter —
// a Postgres table, a write on the path of every failed login, and a purge — and
// it belongs with the rest of the abuse controls in E5 rather than half-built
// here. What this does buy is the thing worth buying now: an online guessing
// attack against one account cannot run at the speed of the network.
//
// Two counters, because one of them is a weapon. A per-account lockout is a
// denial of service anybody can trigger against anybody whose address they
// know, and the fix is not to remove it — a guessing attack against one account
// is the common case — but to tell the two apart. Failures against one account
// from many addresses are somebody attacking the lockout, and that account gets
// a delay rather than a refusal; failures from one address against many
// accounts are somebody working through a list, and that address gets the same.
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
// It lives in contracts rather than in internal/ because the lockout is part of
// what Login promises, so the fake keeps it too and the conformance suite can
// check that both do.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]window
	sources map[string]window
}

type window struct {
	failures int
	until    time.Time
	// from is the distinct addresses this account has failed from inside the
	// window, bounded by MaxSources+1: past the bound the answer is the same
	// whatever else arrives, so nothing more is remembered and a botnet cannot
	// grow the map by trying one more machine.
	from map[string]bool
}

// maxTracked bounds the map. An attacker who tries a fresh invented address
// every time would otherwise fill it: past the bound the expired entries are
// dropped and, if that was not enough, the map is emptied, which costs the
// honest failures their count and costs an attacker nothing they did not
// already have.
const maxTracked = 10_000

// NewLimiter returns an empty limiter.
func NewLimiter() *Limiter {
	return &Limiter{windows: map[string]window{}, sources: map[string]window{}}
}

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
func (l *Limiter) Check(email, ip string) Verdict {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	account, live := l.windows[EmailKey(email)]
	live = live && now.Before(account.until)
	switch {
	case live && account.failures >= MaxAttempts && len(account.from) > MaxSources:
		// Locked, but by a crowd: this is somebody trying to lock the account's
		// owner out, and answering "too many attempts" is doing it for them.
		return Delay
	case live && account.failures >= MaxAttempts:
		return Refuse
	}
	if source, ok := l.sources[ip]; ok && now.Before(source.until) && source.failures >= SourceAttempts {
		// One machine working through a list of addresses. Each account's own
		// counter is untouched, so this is the only thing that sees it.
		return Delay
	}
	return Allow
}

// Failed records one failure, from one address, against one account.
func (l *Limiter) Failed(email, ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	now := time.Now()

	k := EmailKey(email)
	w, ok := l.windows[k]
	if !ok || now.After(w.until) {
		w = window{until: now.Add(AttemptWindow), from: map[string]bool{}}
	}
	w.failures++
	if ip != "" && len(w.from) <= MaxSources {
		w.from[ip] = true
	}
	l.windows[k] = w

	if ip == "" {
		return
	}
	s, ok := l.sources[ip]
	if !ok || now.After(s.until) {
		s = window{until: now.Add(AttemptWindow)}
	}
	s.failures++
	l.sources[ip] = s
}

// Succeeded forgets an address: somebody proved they are who they said. The
// source's count stays, because one correct password out of fifty attempts is
// what a successful guessing run looks like.
func (l *Limiter) Succeeded(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, EmailKey(email))
}

// count records one event of a kind in a window and reports whether it is still
// within max.
//
// The two callers below are what it is for, and both are counts of a string per
// window, which is what sources already holds. space keeps their keys apart
// from an address's — a plain IP never contains a NUL — so this is one map and
// one prune rather than three of each.
func (l *Limiter) count(space, key string, max int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	now := time.Now()
	k := space + "\x00" + key
	w, ok := l.sources[k]
	if !ok || now.After(w.until) {
		w = window{until: now.Add(AttemptWindow)}
	}
	w.failures++
	l.sources[k] = w
	return w.failures <= max
}

// Requested counts one forgotten-password request from an address and reports
// whether it is within ResetRequests for this window. The address is the one
// asking; the address being asked about is never counted, because a cap that
// depended on it would answer the question the route exists not to answer.
func (l *Limiter) Requested(ip string) bool { return l.count("forgot", ip, ResetRequests) }

// Noted reports whether a refusal for this account from this address is worth
// writing down, which is the first one in the window and no more.
//
// A locked-out account under a script is refused before anything is checked,
// and recording each refusal was two audit rows per attempt for a fact that
// does not change: the first one says the account is under attack and the
// nine hundredth says it still is. The failures themselves are still recorded
// one for one — those are attempts that were actually made.
func (l *Limiter) Noted(email, ip string) bool {
	return l.count("noted", EmailKey(email)+"\x00"+ip, 1)
}

// prune drops what has expired, and empties the maps if that was not enough.
// The caller holds the lock.
func (l *Limiter) prune() {
	if len(l.windows)+len(l.sources) < maxTracked {
		return
	}
	now := time.Now()
	for _, m := range []map[string]window{l.windows, l.sources} {
		for k, w := range m {
			if now.After(w.until) {
				delete(m, k)
			}
		}
	}
	if len(l.windows)+len(l.sources) >= maxTracked {
		clear(l.windows)
		clear(l.sources)
	}
}

// EmailKey is the address as this module counts it: one that differs only in
// case or in whitespace is the same account being attacked, and the same
// account in an audit trail. It is exported because the event that records a
// failure has to spell the address the same way the limiter counted it.
func EmailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
