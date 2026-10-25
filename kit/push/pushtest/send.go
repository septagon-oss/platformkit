package pushtest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/push"
)

func sendOp(h Harness) porttest.Op[Fixture] {
	refusals := append(sendRefusals(), providerRefusals(h)...)
	return porttest.Op[Fixture]{
		Name: "Send",
		Ready: func(t *testing.T, f Fixture) uuid.UUID {
			f.Put(t, Healthy)
			return uuid.Nil
		},
		Call: func(f Fixture, _ uuid.UUID) (string, error) {
			receipt, err := f.Adapter.Send(f.Ctx, f.Device(f.Tokens.Carryable), Message())
			if err != nil {
				return "", err
			}
			if receipt.State != h.Answer {
				return "", errors.New(string(f.Platform) + " answers a healthy send with " + string(receipt.State) +
					", and this suite says " + string(h.Answer))
			}
			if f.Requests() == 0 {
				return "", errors.New("the send answered without asking the provider anything")
			}
			return string(receipt.State) + ":" + receipt.ID, nil
		},
		Names:    map[porttest.Kind]string{porttest.Success: "Send: the provider answers a send"},
		Refusals: refusals,
	}
}

func send(f Fixture, token string, m push.Message) error {
	_, err := f.Adapter.Send(f.Ctx, f.Device(token), m)
	return err
}

func sendRefusals() []porttest.Refusal[Fixture] {
	return []porttest.Refusal[Fixture]{
		{
			Kind:  porttest.Named,
			Name:  "Send: a stored token this adapter cannot carry is refused",
			Class: porttest.Correctable,
			Call:  func(f Fixture, _ uuid.UUID) error { return send(f, f.Tokens.Uncarryable, Message()) },
			Is:    isSentinel(push.ErrMalformed),
		},
		{
			Kind:  porttest.Named,
			Name:  "Send: a message with neither a title nor a body is refused",
			Class: porttest.Immutable,
			Call: func(f Fixture, _ uuid.UUID) error {
				m := Message()
				m.Title, m.Body = "", ""
				return send(f, f.Tokens.Carryable, m)
			},
			Is: isSentinel(push.ErrInvalid),
		},
		{
			Kind:  porttest.Named,
			Name:  "Send: a device of another platform is refused",
			Class: porttest.Immutable,
			Call: func(f Fixture, _ uuid.UUID) error {
				d := f.Device(f.Tokens.Carryable)
				d.Platform = otherPlatform(d.Platform)
				_, err := f.Adapter.Send(f.Ctx, d, Message())
				return err
			},
			Is: isSentinel(push.ErrInvalid),
		},
	}
}

func providerRefusals(h Harness) []porttest.Refusal[Fixture] {
	var refusals []porttest.Refusal[Fixture]
	if h.has(Gone) {
		refusals = append(refusals, porttest.Refusal[Fixture]{
			Kind:  porttest.Named,
			Name:  "Send: a token the provider reports gone is ErrGone",
			Class: porttest.Immutable,
			Provoke: func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
				f.Put(t, Gone)
				return row
			},
			Call: func(f Fixture, _ uuid.UUID) error { return send(f, f.Tokens.Carryable, Message()) },
			Is:   isSentinel(push.ErrGone),
		})
	}
	if h.has(Unavailable) {
		refusals = append(refusals, porttest.Refusal[Fixture]{
			Kind: porttest.Named,
			Name: "Send: a provider that is temporarily unavailable is not a gone token",
			Provoke: func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
				f.Put(t, Unavailable)
				return row
			},
			// The class is left out on purpose: a transient failure is not a
			// refusal this port classifies at all, and the assertion is that it is
			// not ErrGone, which is the mistake that deletes a working person's
			// device during a bad hour.
			Call: func(f Fixture, _ uuid.UUID) error { return send(f, f.Tokens.Carryable, Message()) },
			Is: func(_ Fixture, err error) bool {
				return err != nil && !errors.Is(err, push.ErrGone)
			},
		})
	}
	if h.has(Refused) {
		refusals = append(refusals, porttest.Refusal[Fixture]{
			Kind:  porttest.Named,
			Name:  "Send: a provider that refuses the message is ErrRejected",
			Class: porttest.Immutable,
			Provoke: func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
				f.Put(t, Refused)
				return row
			},
			Call: func(f Fixture, _ uuid.UUID) error { return send(f, f.Tokens.Carryable, Message()) },
			Is:   isSentinel(push.ErrRejected),
		})
	}
	return refusals
}
