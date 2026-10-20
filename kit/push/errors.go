package push

import (
	"errors"
	"fmt"
	"strings"
)

// The refusal vocabulary of the port. Anything that is not one of these five is
// transient by contract, and the difference is what a device row costs: see the
// package doc and pushtest's case that a service having a bad afternoon is not a
// gone token.
var (
	// ErrGone is the provider's answer that this token is not a device any
	// more: a Web Push endpoint's 404 or 410 (RFC 8030 §5), an Expo push ticket
	// whose details.error is DeviceNotRegistered. A caller holding the device
	// row removes it — a gone token that is retried is a row that never goes
	// away.
	ErrGone = errors.New("push: the device is gone")
	// ErrMalformed is about the stored token: not a syntax this adapter could
	// carry (an unparseable subscription, an empty or oversized token). The
	// device is kept, because the person's own client can re-register and the
	// next notice will then work.
	ErrMalformed = errors.New("push: the token is not one this adapter can carry")
	// ErrRejected is the provider's refusal of the message as written — a 400
	// from a push endpoint, a ticket that failed for a reason that is not
	// DeviceNotRegistered. The device is kept: nothing about it is wrong.
	ErrRejected = errors.New("push: the provider refused this message")
	// ErrInvalid is our own check refusing before anything is sent:
	// Message.Validate, or an adapter given a device whose platform is not its
	// own.
	ErrInvalid = errors.New("push: this send cannot be made as written")
	// ErrUnsupportedPlatform is Adapters.Send with no adapter behind the
	// device's platform — a fact about the deployment, not about the send.
	ErrUnsupportedPlatform = errors.New("push: this installation carries no such platform")
)

// Redacted is what a secret's place looks like in a sentence an adapter
// returns: the provider wrote the words, but the words are about an address that
// lets anybody push to it.
const Redacted = "[redacted]"

// Redact returns err reporting itself with every occurrence of each secret cut
// out, while still unwrapping to it, so the caller reads the sentinel and the
// cause exactly as it would have.
//
// An adapter owes this on every error that carries a sentence it did not write.
// A provider echoes what it was sent in its refusal — Expo writes "cannot
// deliver to <token>" — and net/http names the URL in a transport failure, which
// for a browser subscription is the subscription's own credential. Those errors
// go into a delivery ledger's reason, into a worker's log line and into a trace
// event, all of which are read by more people than hold the database; a device
// token replays somebody's notices to their phone.
func Redact(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	msg := Scrub(err.Error(), secrets...)
	if msg == err.Error() {
		return err
	}
	return &redacted{msg: msg, err: err}
}

// Scrub is Redact's rule told of a sentence rather than an error: every
// occurrence of each secret in text comes back as Redacted.
//
// It exists because cutting a secret out of a text and shortening that text are
// two operations that must happen in this order. An adapter that shortens a
// provider's refusal to a ledger-sized sentence — which is what a provider
// sentence costs the row that records it — has to cut the credentials while the
// sentence is still whole, because a shortened secret is a secret with a prefix
// nobody can match any more: long enough to use, too short to find again.
func Scrub(text string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, Redacted)
		}
	}
	return text
}

// redacted is an error with its text rewritten and its identity kept.
type redacted struct {
	msg string
	err error
}

func (r *redacted) Error() string { return r.msg }
func (r *redacted) Unwrap() error { return r.err }

// Gone names the provider's reason for a token that is not a device any more:
// the sentence an operator reads in the delivery ledger, wrapped so that
// errors.Is reports ErrGone. The reason is kept out of every span attribute and
// every log line by the transport that records it, and it never carries the
// token.
func Gone(reason string) error { return fmt.Errorf("%w: %s", ErrGone, reason) }
