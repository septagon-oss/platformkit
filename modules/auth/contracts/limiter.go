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
)

// Limiter counts failed logins per address, in this process's memory.
//
// In this process's memory, and that is the honest limit of it: with three
// replicas an attacker gets thirty attempts per window rather than ten, and a
// deploy resets the count. A cluster-wide limit needs a shared counter, which is
// a Postgres table and a purge, and it belongs with the rest of the abuse
// controls in E5 rather than half-built here. What this does buy is the thing
// worth buying now: an online guessing attack against one account cannot run at
// the speed of the network.
//
// It lives in contracts rather than in internal/ because the lockout is part of
// what Login promises, so the fake keeps it too and the conformance suite can
// check that both do.
type Limiter struct {
	mu      sync.Mutex
	windows map[string]window
}

type window struct {
	failures int
	until    time.Time
}

// maxTracked bounds the map. An attacker who tries a fresh invented address
// every time would otherwise fill it: past the bound the expired entries are
// dropped and, if that was not enough, the map is emptied, which costs the
// honest failures their count and costs an attacker nothing they did not
// already have.
const maxTracked = 10_000

// NewLimiter returns an empty limiter.
func NewLimiter() *Limiter { return &Limiter{windows: map[string]window{}} }

// Locked reports whether this address has failed too often to be tried again.
func (l *Limiter) Locked(email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w, ok := l.windows[EmailKey(email)]
	return ok && w.failures >= MaxAttempts && time.Now().Before(w.until)
}

// Failed records one failure and returns whether the address is now locked.
func (l *Limiter) Failed(email string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.prune()
	k := EmailKey(email)
	w, ok := l.windows[k]
	if !ok || time.Now().After(w.until) {
		w = window{until: time.Now().Add(AttemptWindow)}
	}
	w.failures++
	l.windows[k] = w
	return w.failures >= MaxAttempts
}

// Succeeded forgets an address: somebody proved they are who they said.
func (l *Limiter) Succeeded(email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.windows, EmailKey(email))
}

// prune drops what has expired, and empties the map if that was not enough. The
// caller holds the lock.
func (l *Limiter) prune() {
	if len(l.windows) < maxTracked {
		return
	}
	now := time.Now()
	for k, w := range l.windows {
		if now.After(w.until) {
			delete(l.windows, k)
		}
	}
	if len(l.windows) >= maxTracked {
		clear(l.windows)
	}
}

// EmailKey is the address as this module counts it: one that differs only in
// case or in whitespace is the same account being attacked, and the same
// account in an audit trail. It is exported because the event that records a
// failure has to spell the address the same way the limiter counted it.
func EmailKey(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
