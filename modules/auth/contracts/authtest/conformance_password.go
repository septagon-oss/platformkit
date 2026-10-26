package authtest

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func changePassword(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password)
	var keep, other uuid.UUID
	for _, into := range []*uuid.UUID{&keep, &other} {
		session, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		*into = session.ID
	}
	// The wrong current password is refused, and nothing moves: a
	// stolen cookie must not be a stolen account.
	if err := f.Service.ChangePassword(f.Ctx, f.Tx, id, keep, Wrong, "a different passphrase"); !errors.Is(err, contracts.ErrCredentials) {
		t.Fatalf("ChangePassword with the wrong current password = %v, want ErrCredentials", err)
	}
	if sessions(f, id) != 2 {
		t.Errorf("a refused change ended %d of two sessions", 2-sessions(f, id))
	}

	if err := f.Service.ChangePassword(f.Ctx, f.Tx, id, keep, Password, "a different passphrase"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	if _, err := f.Service.Identify(f.Ctx, f.Tx, other, nobody); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the other session survived a password change: %v", err)
	}
	if _, err := f.Service.Identify(f.Ctx, f.Tx, keep, nobody); err != nil {
		t.Errorf("the session that asked for the change was ended too: %v", err)
	}
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", "a different passphrase", nobody); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	// And the machines the change signed out are on the trail: one
	// auth.session_revoked per row its own revocation removed, between the
	// two sign-ins it belongs to. The wrong-password attempt above published
	// nothing, so the only names here are the logins and the one revocation.
	published(t, f, contracts.EventLoggedIn, contracts.EventLoggedIn,
		contracts.EventSessionRevoked, contracts.EventLoggedIn)
}

func requestPasswordReset(t *testing.T, f Fixture) {
	f.User("ada@acme.example.com", Password)
	for _, address := range []string{"ada@acme.example.com", "nobody@acme.example.com"} {
		if err := f.Service.Forget(f.Ctx, f.Tx, address); err != nil {
			t.Errorf("Forget(%q) = %v, want nil however unknown the address", address, err)
		}
	}
	// One event each, and nothing else: no lookup, no token, no mail.
	// An address somebody has and an address nobody has cost the same,
	// which is what stops the public route being an enumeration oracle
	// with a stopwatch — 2.1 ms against 0.9 ms was the measurement that
	// moved the lookup into the worker.
	published(t, f, contracts.EventResetRequested, contracts.EventResetRequested)
	if got := mailed(f); len(got) != 0 {
		t.Errorf("Forget sent %d messages; the request path sends none", len(got))
	}
	if got := notices(f); len(got) != 0 {
		t.Errorf("Forget raised %d notices; the request path raises none", len(got))
	}
}

// NoMailDetail is the sentence an installation whose sender does not deliver
// answers a door that would have to promise a person an email with. It is spelled
// once here because every implementation of this contract answers in it, and a
// case that quoted it twice could drift from what either one says.
const NoMailDetail = "account email delivery is unavailable"

// refuseResetWithoutMail is the forgotten-password door standing in the
// installation with no mail host. The acknowledgment this route answers with
// promises a link, so where the sender files the message instead of dialling the
// door refuses — and a refused mutation writes nothing and publishes nothing.
//
// The claim belongs to the contract rather than to one implementation: the fake
// and the SQL-backed service are both run against this case, so neither can keep
// a promise the other breaks.
func refuseResetWithoutMail(t *testing.T, f Fixture) {
	f.User("ada@acme.example.com", Password)
	if f.Delivering == nil {
		t.Fatal("this harness cannot say whether its sender attempts a message")
	}
	f.Delivering(false)
	var refused *problem.Problem
	err := f.Service.Forget(f.Ctx, f.Tx, "ada@acme.example.com")
	if !errors.As(err, &refused) {
		t.Fatalf("Forget with no mail host returned %v, want the unavailable refusal", err)
	}
	if refused.GetStatus() != http.StatusServiceUnavailable || refused.Detail != NoMailDetail {
		t.Errorf("Forget refused with %d %q, want the deployment's own unavailable sentence", refused.GetStatus(), refused.Detail)
	}
	// The same answer about an address nobody has: the refusal is a fact about the
	// installation, so it cannot say which address has an account.
	if err := f.Service.Forget(f.Ctx, f.Tx, "nobody@acme.example.com"); !errors.As(err, &refused) || refused.GetStatus() != http.StatusServiceUnavailable {
		t.Errorf("an unknown address was answered differently from a known one: %v", err)
	}
	published(t, f)
	if got := mailed(f); len(got) != 0 {
		t.Errorf("a refused reset sent %d messages, want none", len(got))
	}
	if got := notices(f); len(got) != 0 {
		t.Errorf("a refused reset raised %d notices, want none", len(got))
	}
	// The same request, the same address, one mail host later: accepted, and it is
	// the request's own event that says so.
	f.Delivering(true)
	if err := f.Service.Forget(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
		t.Fatalf("with a mail host the same request was refused: %v", err)
	}
	published(t, f, contracts.EventResetRequested)
}

func mailPasswordReset(t *testing.T, f Fixture) {
	f.User("ada@acme.example.com", Password)
	for _, address := range []string{"ada@acme.example.com", "nobody@acme.example.com"} {
		if err := f.Service.Reissue(f.Ctx, f.Tx, address); err != nil {
			t.Errorf("Reissue(%q) = %v, want nil however unknown the address", address, err)
		}
	}
	// The link leaves on the commit of the transaction that minted it, so the
	// step below is the first place a mailbox can answer for it.
	settle(t, f, func(f Fixture) {
		sent := mailed(f)
		// One credential left, for the address that is here. The other message is
		// the sentence that says no link was sent to the address that is not — a
		// message every implementation of this contract owes a request that names
		// an address, because the public delivery door answers a caller only from
		// the record its own call caused. It carries nothing, which is what the
		// loop below is for.
		var links []notificationcontracts.Message
		for _, m := range sent {
			if TokenIn(m.Body) != "" {
				links = append(links, m)
			}
		}
		if len(links) != 1 {
			t.Fatalf("Reissue mailed %d credentials, want one — for the address that is here", len(links))
		}
		sent = links
		if sent[0].To != "ada@acme.example.com" || TokenIn(sent[0].Body) == "" {
			t.Errorf("the message is %+v, want one to Ada carrying a token", sent[0])
		}
		for _, m := range mailed(f) {
			if m.To != "ada@acme.example.com" && TokenIn(m.Body) != "" {
				t.Errorf("a credential left for %s, which has no account to hold it: %q", m.To, m.Subject)
			}
		}
		// And the notice raised beside it carries no token. A notification
		// is an ordinary row — listed by a route, readable by anybody who
		// can read the table — so a link with a secret in it is a live
		// credential sitting in one. It used to be exactly that.
		for _, n := range notices(f) {
			if TokenIn(n.Link) != "" || strings.Contains(n.Body, TokenIn(sent[0].Body)) {
				t.Errorf("the notice carries the token: %+v", n)
			}
		}
		// Nothing about it reaches the outbox from here either: this module
		// publishes when a password is reset, not when somebody asks.
		published(t, f)
	})
}

func rateLimitPasswordLinks(t *testing.T, f Fixture) {
	f.User("ada@acme.example.com", Password)
	for range 5 {
		if err := f.Service.Reissue(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
			t.Fatalf("Reissue: %v", err)
		}
	}
	// A public route that costs a mail is somebody else's inbox filled
	// by a stranger unless the recipient is capped too. The cap is on
	// the person being written to; the route's own cap is on the
	// address doing the asking.
	settle(t, f, func(f Fixture) {
		if got := mailed(f); len(got) != 1 {
			t.Errorf("five requests sent %d messages, want one", len(got))
		}
	})
}

func resetPassword(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password)
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := f.Service.Reissue(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
		t.Fatalf("Reissue: %v", err)
	}
	// The link is in the person's mail from the commit onwards, and the rest of
	// the case spends it there.
	settle(t, f, func(f Fixture) {
		sent := mailed(f)
		if len(sent) != 1 {
			t.Fatalf("Reissue sent %d messages, want one", len(sent))
		}
		token := TokenIn(sent[0].Body)

		session, err := f.Service.Reset(f.Ctx, f.Tx, token, "a different passphrase", nobody)
		if err != nil {
			t.Fatalf("Reset: %v", err)
		}
		if session == nil {
			t.Fatal("Reset opened no session: the person who spent the link is back at the sign-in form")
		}
		// One, and it is this one. The session the Login above opened is gone —
		// the internal service's own case refuses that row by asking it (/me with
		// the session that was live before the reset) — and what remains is the one
		// the link itself opened. A reset that signed this browser in without ending
		// the others would answer the same count, which is why the count is not the
		// whole of the promise and the contract's sentence says both halves.
		if got := sessions(f, id); got != 1 {
			t.Errorf("%d sessions after a reset, want the one it opened", got)
		}
		// Once. The row is deleted rather than flagged, so the second
		// attempt is refused by the same answer an invented token gets.
		for _, second := range []string{token, "not a token at all", ""} {
			if _, err := f.Service.Reset(f.Ctx, f.Tx, second, "another passphrase", nobody); !errors.Is(err, contracts.ErrCredentials) {
				t.Errorf("Reset(%q) = %v, want ErrCredentials", second, err)
			}
		}
		published(t, f, contracts.EventLoggedIn, contracts.EventSessionRevoked,
			contracts.EventPasswordReset, contracts.EventLoggedIn)
		if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", "a different passphrase", nobody); err != nil {
			t.Errorf("the reset password does not work: %v", err)
		}
	})
}

func offerInvitationLink(t *testing.T, f Fixture) {
	invited := f.User("invited@acme.example.com", "")
	active := f.User("ada@acme.example.com", Password)
	for _, id := range []uuid.UUID{invited, active, uuid.New()} {
		if err := f.Service.Offer(f.Ctx, f.Tx, id); err != nil {
			t.Errorf("Offer(%s) = %v, want nil", id, err)
		}
	}
	// An invitation is mailed once its row is committed, and the link it carries
	// is spent from the settle step, where the mail is.
	settle(t, f, func(f Fixture) {
		if got := mailed(f); len(got) != 1 {
			t.Fatalf("Offer sent %d messages, want one — for the person who cannot sign in", len(got))
		}
		// And the link works: an invitation and a reset are one mechanism.
		token := TokenIn(mailed(f)[0].Body)
		if _, err := f.Service.Reset(f.Ctx, f.Tx, token, "a chosen passphrase", nobody); err != nil {
			t.Fatalf("Reset with an invitation's token: %v", err)
		}
		if _, _, err := f.Service.Login(f.Ctx, f.Tx, "invited@acme.example.com", "a chosen passphrase", nobody); err != nil {
			t.Errorf("the invited person cannot sign in: %v", err)
		}
	})
}
