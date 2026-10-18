package inference

import (
	"context"
	"errors"
	"time"
)

// The three bounds an ask may not exceed, and the one thing they buy.
//
// They are constants rather than configuration because a bound an operator can
// raise is a default, and these three are the sizes this kit already answers
// elsewhere: MaxImageBytes is httpx.MaxBodyBytes, so the picture a person
// uploads and the picture the kit forwards are one size, and a prompt that needs
// more room than MaxPromptBytes is a document, which is billed per call and is
// the caller's to chunk.
const (
	MaxImageBytes       = 1 << 20
	MaxPromptBytes      = 32 << 10
	MaxInstructionBytes = 4 << 10

	// defaultAnswerBytes and defaultTimeout are the two figures a composition
	// gets when it names none: an answer the length of a page of prose, and a
	// wire that may take a slow model's time without ever taking an infinite one.
	defaultAnswerBytes = 8 << 10
	defaultTimeout     = 60 * time.Second

	// PracticeModel is what an engine with no endpoint names itself, so a person
	// can be told "practice mode" without an application reading internals.
	PracticeModel = "practice"
)

// Engine asks a model one question about text and at most one picture.
//
// One Engine is one model at one endpoint; two models are two engines. The
// guarantees below belong to the engine and hold for every adapter: a Provider
// implements the wire and nothing else.
type Engine interface {
	// Chat sends one ask and returns the answer. It honours the caller's
	// deadline and cancellation, spends the tenant's allowance before the wire,
	// and never sends what the call refused to count. A refusal returns no
	// answer: a half translation and a guess cut off mid-word are data
	// corruption with a green check mark.
	Chat(ctx context.Context, ask Request) (Answer, error)

	// Model names the model this engine puts on the wire — the answer to "am I
	// talking to a model or to the fake?".
	Model() string

	// Close releases what the adapter opened. It is safe to call twice; the
	// second answer is ErrClosed, and so is every Chat after the first Close.
	Close() error
}

// Request is one ask: the steer, the question, and at most one picture.
type Request struct {
	// Instruction is the caller's steer, sent in the system role. It is separate
	// from Prompt because the two have different owners: the steer is a constant
	// of the calling package and the prompt is whatever the person typed or
	// uploaded. A caller that splices its steer into the prompt puts an untrusted
	// picture next to the rule that is supposed to constrain it.
	Instruction string
	// Prompt is the question, in the user role. Required.
	Prompt string
	// Image is a PNG encoding, or nil for a text-only ask. Nothing else is
	// accepted, because a port that guesses an encoding reads a JPEG's first
	// megabyte as a picture of nothing.
	Image []byte
}

// Answer is what the model said, whole or not at all.
type Answer struct {
	// Text is never empty when Chat returned no error.
	Text string
	// Model is the model as the provider named it, "" when it named none.
	Model string
	Usage Usage
}

// Usage is the token count the provider reported.
type Usage struct {
	InputTokens  int
	OutputTokens int
	// Reported is true on every successful Answer. Missing or negative token
	// counts refuse the response with ErrAnswerShape; no count is invented.
	Reported bool
}

// Provider is what an adapter implements: the wire, and nothing else.
//
// Every guarantee — the bounds, the cap, the span, the completeness of the
// answer — belongs to New, so an adapter cannot get one of them wrong on its own
// and a second adapter is one file rather than one re-implementation.
type Provider interface {
	Chat(ctx context.Context, ask Request) (Response, error)
	Close() error
}

// Response is one answer as the wire gave it.
type Response struct {
	Text  string
	Model string
	// Finish is the wire's own token ("stop", "length", "content_filter", …),
	// copied verbatim, "" when the response carried none.
	Finish string
	// Truncated is the adapter's reading of that token: the provider said it cut
	// this answer short.
	Truncated bool
	Usage     Usage
}

// The refusals. Each names who can fix it, in the sentence it earns; the table
// in README.md is the one place the whole set, its class and its correcter are
// stated together.
var (
	// ErrNoTenant is an ask that belongs to no tenant. Nobody at runtime can fix
	// it: the caller resolved no tenant, and one shared bucket would let one
	// tenant spend the installation's allowance and refuse strangers. A job or a
	// delivery resolves its tenant before it asks.
	ErrNoTenant = errors.New("inference: this ask belongs to no tenant")

	// ErrAskShape is an ask with no question. It wraps fault.ErrInvalid rather
	// than declaring a fourth fault sentinel, which kit/fault refuses.
	ErrAskShape = errors.New("inference: this ask has no question")

	// ErrAskTooLarge is an ask that is a document; the caller shortens or chunks
	// it. ErrImageTooLarge and ErrImageFormat are the picture's two versions of
	// the same correctable fact.
	ErrAskTooLarge   = errors.New("inference: this ask is longer than this kit sends")
	ErrImageTooLarge = errors.New("inference: this picture is larger than this kit sends")
	ErrImageFormat   = errors.New("inference: this picture is not a PNG")

	// ErrSpent is the tenant's daily allowance, spent: correctable by the
	// operator raising the cap, and retriable by the caller after the RetryAfter
	// a *SpentError carries. It is refused by name and never silently.
	ErrSpent = errors.New("inference: this tenant's inference allowance is spent")

	// ErrSpentUnproven is the counter's queue: the row's lock was not ours, so
	// nothing reached the row and nothing about the window is known. Its sentence
	// is not "spent", because claiming the allowance is empty would be a lie with
	// a number on it.
	ErrSpentUnproven = errors.New("inference: this tenant's inference allowance could not be counted just now")

	// ErrCounterUnavailable is the counter's outage. An inference allowance fails
	// closed here, as kit/httpx fails its access counter closed: an uncounted
	// model call is a bill nobody authorised.
	ErrCounterUnavailable = errors.New("inference: the inference allowance could not be counted")

	// ErrAnswerEmpty, ErrAnswerIncomplete and ErrAnswerTooLarge are an answer
	// that cannot be returned: the model said nothing, said it stopped before it
	// finished, or said more than this installation will hold. All three return
	// no text at all, and all three leave the call counted — the provider spent
	// its tokens, and a refusal that un-counted them would make a chatty endpoint
	// free to hammer.
	ErrAnswerEmpty      = errors.New("inference: the model answered nothing")
	ErrAnswerIncomplete = errors.New("inference: the model stopped before it finished")
	ErrAnswerTooLarge   = errors.New("inference: the answer is larger than this installation's limit")

	// ErrAnswerShape is an endpoint that does not answer this port's shape;
	// ErrResponseTooLarge is the same installation's bound on bytes read.
	ErrAnswerShape      = errors.New("inference: the answer is not in the shape this port reads")
	ErrResponseTooLarge = errors.New("inference: the response is larger than this installation's limit")

	// ErrProviderRefused is a 4xx and ErrProviderUnavailable a 5xx or a failed
	// dial. Neither quotes the response body: a vendor's error can echo the
	// prompt it was sent, which is how a person's drawing reaches a log.
	ErrProviderRefused     = errors.New("inference: the endpoint refused this ask")
	ErrProviderUnavailable = errors.New("inference: the endpoint could not answer")

	// ErrNoScript is an engine built from fewer scripted replies than it was
	// asked for: whoever scripted it is told, rather than being handed "".
	ErrNoScript = errors.New("inference: this engine was asked more times than it was scripted")

	// ErrClosed is an engine that has been closed, and a process releasing its
	// resources.
	ErrClosed = errors.New("inference: this engine is closed")
)

// SpentError is a refused ask that carries what its refusal knows: how long the
// caller should wait before asking again. errors.Is over it reaches ErrSpent or
// ErrSpentUnproven, and RetryAfter is whole seconds rounded up by whoever turns
// it into a response header.
type SpentError struct {
	err        error
	RetryAfter time.Duration
}

func (e *SpentError) Error() string { return e.err.Error() }
func (e *SpentError) Unwrap() error { return e.err }

// Class names the closed telemetry class of this refusal, so a door that reports
// it counts a spent allowance and an unprovable one apart.
func (e *SpentError) Class() string { return classOf(e.err) }

func newSpent(err error, after time.Duration) *SpentError {
	if after < 0 {
		after = 0
	}
	return &SpentError{err: err, RetryAfter: after}
}
