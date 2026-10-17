package push

import (
	"errors"
	"fmt"
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

// Gone names the provider's reason for a token that is not a device any more:
// the sentence an operator reads in the delivery ledger, wrapped so that
// errors.Is reports ErrGone. The reason is kept out of every span attribute and
// every log line by the transport that records it, and it never carries the
// token.
func Gone(reason string) error { return fmt.Errorf("%w: %s", ErrGone, reason) }
