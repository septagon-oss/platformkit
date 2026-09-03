package contracts_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The suite runs over kit/limit's memory store. What the store does is proved
// against Postgres in kit/limit's own tests, and against both implementations
// at once; what is proved here is the shape of the verdict, which is this
// module's own rule and not the store's.
func newLimiter() (*contracts.Limiter, context.Context) {
	return contracts.NewLimiter(limit.Memory()), context.Background()
}

// TestTheLockoutIsNotAWeapon is the whole reason there are two counters.
//
// A per-account lockout is a denial of service anybody can trigger against
// anybody whose address they know: ten wrong passwords from a botnet, once a
// quarter of an hour, for as long as they care to. Refusing is right when the
// failures came from one or two places — that is somebody guessing a password —
// and wrong when they came from many, which is somebody attacking the lockout
// rather than the account.
func TestTheLockoutIsNotAWeapon(t *testing.T) {
	t.Run("one place guessing one password is refused", func(t *testing.T) {
		l, ctx := newLimiter()
		for range contracts.MaxAttempts {
			l.Failed(ctx, "ada@acme.example.com", "203.0.113.1")
		}
		if got := l.Check(ctx, "ada@acme.example.com", "203.0.113.1"); got != contracts.Refuse {
			t.Errorf("Check after %d failures from one address = %v, want Refuse", contracts.MaxAttempts, got)
		}
		// And another account is untouched: the limit is per account, so one
		// person under attack does not lock out everybody else.
		if got := l.Check(ctx, "grace@acme.example.com", "203.0.113.1"); got != contracts.Allow {
			t.Errorf("another account = %v, want Allow", got)
		}
	})

	t.Run("many places against one account get a delay, not a lockout", func(t *testing.T) {
		l, ctx := newLimiter()
		for i := range contracts.MaxAttempts + contracts.MaxSources {
			l.Failed(ctx, "ada@acme.example.com", ip(i))
		}
		got := l.Check(ctx, "ada@acme.example.com", "198.51.100.7")
		if got == contracts.Refuse {
			t.Fatal("a distributed attack locked the account's owner out, which is doing the attacker's work")
		}
		if got != contracts.Delay {
			t.Errorf("Check under a distributed attack = %v, want Delay", got)
		}
	})

	t.Run("one place against many accounts gets a delay", func(t *testing.T) {
		l, ctx := newLimiter()
		for i := range contracts.SourceAttempts {
			l.Failed(ctx, account(i), "203.0.113.9")
		}
		// No account's own counter has been touched — one failure each — so
		// this is the only thing that sees the run.
		if got := l.Check(ctx, "nobody@acme.example.com", "203.0.113.9"); got != contracts.Delay {
			t.Errorf("Check from an address working through a list = %v, want Delay", got)
		}
		if got := l.Check(ctx, "nobody@acme.example.com", "203.0.113.10"); got != contracts.Allow {
			t.Errorf("Check from somewhere else = %v, want Allow", got)
		}
	})

	t.Run("a correct password forgets the account and not the source", func(t *testing.T) {
		l, ctx := newLimiter()
		for range contracts.MaxAttempts {
			l.Failed(ctx, "ada@acme.example.com", "203.0.113.1")
		}
		l.Succeeded(ctx, "ada@acme.example.com")
		if got := l.Check(ctx, "ada@acme.example.com", "203.0.113.1"); got != contracts.Allow {
			t.Errorf("Check after a success = %v, want Allow", got)
		}
	})
}

func ip(i int) string { return "203.0.113." + string(rune('a'+i%26)) }
func account(i int) string {
	return string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@acme.example.com"
}
