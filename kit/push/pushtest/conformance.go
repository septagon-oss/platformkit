// Package pushtest checks push adapters through kit/porttest. Harness supplies
// tokens, provider states and fixtures; Run exercises delivery and named refusals,
// including transient outages that must not be treated as gone devices.
// Operations write no repository rows; Suite.Own covers assertions outside the descriptors.
package pushtest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/push"
)

// State is what the party an adapter talks to is made to answer. A stand-in is
// Healthy until a case puts it somewhere else.
type State int

const (
	// Healthy is the provider taking the message.
	Healthy State = iota
	// Gone is the provider saying this token is not a device any more.
	Gone
	// Unavailable is the provider having a bad afternoon: a 429, a 5xx, a
	// dropped connection.
	Unavailable
	// Refused is the provider refusing the message as written.
	Refused
)

// Tokens are the spellings a case hands an adapter, named for what the case is
// about rather than for what the bytes are.
type Tokens struct {
	// Carryable is a token of this platform the stand-in will take.
	Carryable string
	// Uncarryable is a stored token this adapter could not carry whatever the
	// provider thinks: empty, a pasted error page, a blob.
	Uncarryable string
	// ForeignPlatform is a well-formed token of some other platform. Empty when
	// the suite has none to offer, which is what leaves the case out.
	ForeignPlatform string
	// PlainHTTPEndpoint is a subscription whose endpoint is http rather than
	// https. Empty except for a platform whose token is a URL.
	PlainHTTPEndpoint string
}

// Span is what one send left on the trace.
type Span struct {
	Name  string
	Attrs map[string]string
}

// Fixture is one case's world: the adapter, the party it talks to, and the
// reads a case makes on both. Every closure below is supplied by the provider's
// own test, because only it knows whether the stand-in is an httptest server or
// an in-memory recorder.
type Fixture struct {
	Ctx      context.Context
	Adapter  push.Adapter
	Platform push.Platform
	Tokens   Tokens

	// Device is the device a case sends to, with the tenant and person the span
	// is asserted to name.
	Device func(token string) push.Device
	// Router is the composition's view of this adapter, which is what a send
	// routes through to get its span.
	Router func() push.Adapters
	// Stand is the party the adapter talks to, as this package's own type: a
	// case of a provider's own reaches its stand-in through it.
	Stand any
	// Put moves the party the adapter talks to into a state.
	Put func(t *testing.T, s State)
	// Requests is how many requests have reached the party.
	Requests func() int
	// LastBody is the payload of the last request that reached it, and "" where
	// the stand-in cannot see the bytes.
	LastBody func() string
	// Spans is what the sends of this case left on the trace, and Lines is what
	// they wrote to the log. Every world supplies both: the trace and the log are
	// two of the three places a device token can leak out of, and a suite that
	// could read neither would say nothing about the third.
	Spans func() []Span
	Lines func() []string
}

// Message is the notice every case sends: the words arrive rendered, and the
// cases that change one do so by hand.
func Message() push.Message {
	return push.Message{
		Title: "The roof is open",
		Body:  "The roof is open until six.",
		URL:   "https://example.test/roof",
		Tag:   "roof-1",
		TTL:   time.Hour,
	}
}

// Harness is what a provider's test supplies: the platform, the tokens and the
// world. Answer is the receipt state a healthy send gives — a Web Push endpoint
// delivers, Expo's push service accepts — and States lists the states its
// stand-in can be put into, which is what decides which of the port's refusals
// the suite can provoke. A recorder that always answers can only be asked the
// ones that need no answer at all.
type Harness struct {
	Port   string
	Answer push.State
	Tokens Tokens
	States []State
	World  func(t *testing.T, run func(Fixture))
	// Extra is the provider's own cases, run after the shared ones with the
	// reason each is hand-written.
	Extra []porttest.Case[Fixture]
}

func (h Harness) has(s State) bool {
	for _, ok := range h.States {
		if ok == s {
			return true
		}
	}
	return false
}

// Run is the suite: the cases every adapter owes, then the provider's own.
func Run(t *testing.T, h Harness) {
	t.Helper()
	porttest.Run(t, Suite(h))
}

// Suite is the description, exposed so a provider can pin its case names with
// porttest.Names without running them.
func Suite(h Harness) porttest.Suite[Fixture] {
	return porttest.Suite[Fixture]{
		Port:  h.Port,
		World: h.World,
		// A send publishes nothing of its own: an adapter holds no outbox, and
		// the module that raised the notice is the party that records it.
		Events:   func(Fixture) []string { return nil },
		Classify: classify,
		Ops:      []porttest.Op[Fixture]{validateOp(h), sendOp(h)},
		Own:      ownCases(h),
	}
}

// classify is the port's own reading of a refusal. The rule is the one the
// package doc states: a token nobody can carry is the one thing the caller's own
// client can put right, and every other refusal is a fact about the provider or
// about the message.
func classify(err error) porttest.Class {
	switch {
	case errors.Is(err, push.ErrMalformed):
		return porttest.Correctable
	case errors.Is(err, push.ErrGone), errors.Is(err, push.ErrRejected),
		errors.Is(err, push.ErrInvalid), errors.Is(err, push.ErrUnsupportedPlatform):
		return porttest.Immutable
	default:
		return porttest.Unclassified
	}
}

func validateOp(h Harness) porttest.Op[Fixture] {
	refusals := []porttest.Refusal[Fixture]{
		{
			Kind:  porttest.Named,
			Name:  "Validate: an empty token is refused",
			Class: porttest.Correctable,
			Call:  func(f Fixture, _ uuid.UUID) error { return f.Adapter.Validate("") },
			Is:    isSentinel(push.ErrMalformed),
		},
		{
			Kind:  porttest.Named,
			Name:  "Validate: a token this adapter cannot carry is refused",
			Class: porttest.Correctable,
			Call:  func(f Fixture, _ uuid.UUID) error { return f.Adapter.Validate(f.Tokens.Uncarryable) },
			Is:    isSentinel(push.ErrMalformed),
		},
	}
	if h.Tokens.ForeignPlatform != "" {
		refusals = append(refusals, porttest.Refusal[Fixture]{
			Kind:  porttest.Named,
			Name:  "Validate: another platform's token is refused",
			Class: porttest.Correctable,
			Call:  func(f Fixture, _ uuid.UUID) error { return f.Adapter.Validate(f.Tokens.ForeignPlatform) },
			Is:    isSentinel(push.ErrMalformed),
		})
	}
	if h.Tokens.PlainHTTPEndpoint != "" {
		refusals = append(refusals, porttest.Refusal[Fixture]{
			Kind:  porttest.Named,
			Name:  "Validate: a subscription at an http endpoint is refused",
			Class: porttest.Correctable,
			Call:  func(f Fixture, _ uuid.UUID) error { return f.Adapter.Validate(f.Tokens.PlainHTTPEndpoint) },
			Is:    isSentinel(push.ErrMalformed),
		})
	}
	return porttest.Op[Fixture]{
		Name: "Validate",
		Ready: func(t *testing.T, f Fixture) uuid.UUID {
			if err := f.Adapter.Validate(f.Tokens.Carryable); err != nil {
				t.Fatalf("%s: the suite's own stand-in token is not one the adapter carries: %v", h.Port, err)
			}
			return uuid.Nil
		},
		Call: func(f Fixture, _ uuid.UUID) (string, error) {
			if err := f.Adapter.Validate(f.Tokens.Carryable); err != nil {
				return "", err
			}
			return "carryable", nil
		},
		Names:    map[porttest.Kind]string{porttest.Success: "Validate: a token of this platform is accepted"},
		Refusals: refusals,
	}
}

// ownCases are the three assertions a description cannot express, added to
// whatever the provider's own suite brings. They are shared rather than copied
// because the span, the token and the clipped body are the port's contract with
// an operator and with a person's phone, and an adapter is not free to keep them
// differently.
func ownCases(h Harness) []porttest.Case[Fixture] {
	// The three cases below are shared rather than copied: the span, the token
	// and the clipped body are the port's contract with an operator and with a
	// person's phone, and an adapter is not free to keep them differently.
	var own []porttest.Case[Fixture]
	own = append(own,
		porttest.Case[Fixture]{
			Name: "Send: the send puts one span on the trace",
			Because: "the description names no span reader, and an operation the harness can see is a " +
				"method call, not a trace",
			Run: func(t *testing.T, f Fixture) {
				before := f.Requests()
				_, err := f.Router().Send(f.Ctx, f.Device(f.Tokens.Carryable), Message())
				if err != nil {
					t.Fatalf("routed send: %v", err)
				}
				if f.Requests() != before+1 {
					t.Errorf("the routed send left %d requests behind, want one", f.Requests()-before)
				}
				spans := f.Spans()
				if len(spans) != 1 {
					t.Fatalf("the send left %d spans on the trace, want one: %v", len(spans), spans)
				}
				if spans[0].Name != "push send" {
					t.Errorf("the span is named %q, want \"push send\"", spans[0].Name)
				}
				d := f.Device(f.Tokens.Carryable)
				want := map[string]string{
					"push.platform": string(d.Platform),
					"push.tenant":   d.TenantID.String(),
					"push.device":   d.ID.String(),
					"push.state":    string(h.Answer),
				}
				for k, v := range want {
					if got := spans[0].Attrs[k]; got != v {
						t.Errorf("the span carries %s=%q, want %q", k, got, v)
					}
				}
			},
		},
		porttest.Case[Fixture]{
			Name: "Send: the token is not in any span attribute or log line",
			Because: "the assertion is over the recorders, which no field of the harness's Op carries, and " +
				"it is the one case that decides whether a trace is a keystore",
			Run: func(t *testing.T, f Fixture) {
				if _, err := f.Router().Send(f.Ctx, f.Device(f.Tokens.Carryable), Message()); err != nil {
					t.Fatalf("routed send: %v", err)
				}
				for _, s := range f.Spans() {
					for k, v := range s.Attrs {
						if strings.Contains(v, f.Tokens.Carryable) {
							t.Errorf("the span %q carries %s=%q, which holds the device token", s.Name, k, v)
						}
					}
				}
				for _, line := range f.Lines() {
					if strings.Contains(line, f.Tokens.Carryable) {
						t.Errorf("the log line %q carries the device token in full", line)
					}
				}
			},
		},
		porttest.Case[Fixture]{
			Name: "Send: a body longer than MaxBody arrives clipped and says so",
			Because: "the description asserts that a call succeeded; what reached the provider's socket and " +
				"what the span says about it are the contents of a success, which no Op field carries",
			Run: func(t *testing.T, f Fixture) {
				long := Message()
				long.Body = strings.Repeat("é", push.MaxBody)
				if _, err := f.Router().Send(f.Ctx, f.Device(f.Tokens.Carryable), long); err != nil {
					t.Fatalf("routed send of a long notice: %v", err)
				}
				if body := f.LastBody(); body != "" && len([]byte(body)) > 4*push.MaxBody {
					t.Errorf("the payload reached the provider at %d bytes, which no transport carries", len([]byte(body)))
				}
				var truncated bool
				for _, s := range f.Spans() {
					if s.Attrs["push.truncated"] == "true" {
						truncated = true
					}
				}
				if !truncated {
					t.Errorf("no span said push.truncated about a notice clipped from %d bytes", len(long.Body))
				}
			},
		})
	return append(own, h.Extra...)
}

// hasGoneSpanReader is whether this world can see a trace and a log at all: the
// three shared own-cases need both, and a provider whose stand-in is a bare
// recorder without them says so by leaving the closures out rather than by
// skipping a green case.
func isSentinel(want error) func(Fixture, error) bool {
	return func(_ Fixture, err error) bool { return errors.Is(err, want) }
}

// otherPlatform is a platform of the port that is not this one, for the case
// that hands an adapter a device it has no business carrying.
func otherPlatform(p push.Platform) push.Platform {
	for _, known := range push.Platforms {
		if known != p {
			return known
		}
	}
	return push.Platform("")
}
