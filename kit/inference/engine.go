package inference

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.38.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Bucket is the counter key every ask spends, and the whole of what this package
// names a bucket by. It carries no caller text: an ask whose bucket a caller
// could name could mint its own allowance. The counter scopes it to the tenant of
// the context — kit/limit's own scoping, whose tenant segment is fixed-width — so
// two customers never spend one another's allowance and no caller has to remember
// to say whose call this was.
const Bucket = "inference"

// Counter is the half of kit/limit's Limiter this package asks of a composition,
// declared here rather than imported for the reason kit/httpx declares its
// WriteLimiter: a package that counts calls should not link a database runner to
// get the number. limit.Limiter — Postgres or in-memory, scoped by limit.PostgresFor
// if the installation runs more than one app — satisfies it as it is.
//
// Both methods take the tenant-scoped key from the context the caller passed, and
// both are read ahead of the wire: an allowance spent on a call that was never
// made is a bill for nothing.
type Counter interface {
	Count(ctx context.Context, key string, window time.Duration) (n int, retryAfter time.Duration, err error)
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)
}

// Config is one endpoint, one model and one allowance. The composition builds it
// once, before anything is served; there is no reload, because an allowance that
// changed mid-request is a cap nobody can state to the person it refused.
type Config struct {
	// Model is what the ask names on the wire. Required: at most 128 bytes, no
	// space and no control character, because the value goes into a span name and
	// a JSON body.
	Model string

	// Provider is the gen_ai.provider.name this endpoint is measured under. It
	// defaults to "openai". The convention's pinned registry carries no value for
	// a local Ollama, vLLM or LiteLLM endpoint, and says the attribute "may differ
	// from the actual model provider", so a lower-case slug of your own is
	// accepted: refusing one would refuse exactly the local deployment.
	Provider string

	// DailyCalls is the tenant's allowance per Window. New requires a positive
	// allowance and a Counter. Only Practice can construct an uncapped engine.
	DailyCalls int

	// Window is what DailyCalls counts over. It defaults to 24 hours: a fixed
	// window on the store's clock, so "daily" means "per 24 h per tenant" and
	// there is no local midnight to get wrong.
	Window time.Duration

	// MaxAnswerBytes bounds the answer a caller may see; MaxResponseBytes bounds
	// the bytes the adapter reads. The response bound is the larger, or a
	// legitimate answer is refused by the envelope around it.
	MaxAnswerBytes   int
	MaxResponseBytes int

	// Timeout is this installation's backstop against an endpoint that never
	// answers. It is applied as a deadline on the caller's context, so it can only
	// ever shorten it, never lengthen it: the caller's own deadline stays the real
	// one. Defaults to 60 seconds.
	Timeout time.Duration

	// Counter rations the calls. Required when DailyCalls is positive: a cap with
	// no counter is a cap nobody spends.
	Counter Counter
}

// Defaults returns the configuration with the figures a composition may leave
// unnamed filled in, and refuses the ones that cannot be satisfied together. It
// touches nothing but the copy it returns.
//
// It is exported because an adapter is built from the same Config before the engine
// is, and reads two of these bounds: MaxResponseBytes it reads off a zero Config is
// a bound that refuses every answer the endpoint could send, which is a failure no
// caller can tell from an endpoint that is broken.
func (c Config) Defaults() (Config, error) {
	if err := c.defaults(); err != nil {
		return Config{}, err
	}
	return c, nil
}

// defaults is Defaults, on the receiver a mutation can land on.
func (c *Config) defaults() error {
	if c.Model == "" || len(c.Model) > 128 || strings.ContainsAny(c.Model, " \t\n\r") || strings.ContainsFunc(c.Model, func(r rune) bool { return r < 32 || r == 127 }) {
		return fmt.Errorf("inference: model %q is not a name a span and a JSON body can both carry", c.Model)
	}
	if c.Provider == "" {
		c.Provider = semconv.GenAIProviderNameOpenAI.Value.AsString()
	}
	if !providerName.MatchString(c.Provider) {
		return fmt.Errorf("inference: provider %q is not a lower-case slug of letters, digits, dots, underscores and dashes", c.Provider)
	}
	if c.DailyCalls < 0 {
		return fmt.Errorf("inference: daily calls %d is not an allowance", c.DailyCalls)
	}
	if c.DailyCalls > 0 && c.Counter == nil {
		return errors.New("inference: an allowance with no counter spends nothing")
	}
	if c.DailyCalls <= 0 && c.Counter != nil {
		return errors.New("inference: a counter with no allowance counts nothing")
	}
	if c.Window <= 0 {
		c.Window = 24 * time.Hour
	}
	if c.MaxAnswerBytes <= 0 {
		c.MaxAnswerBytes = defaultAnswerBytes
	}
	if c.MaxResponseBytes == 0 {
		c.MaxResponseBytes = 8 * c.MaxAnswerBytes
	}
	if c.MaxResponseBytes <= c.MaxAnswerBytes {
		return fmt.Errorf("inference: a %d-byte response cannot hold a %d-byte answer", c.MaxResponseBytes, c.MaxAnswerBytes)
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultTimeout
	}
	return nil
}

// providerName is the grammar Config.Provider accepts. It admits every value the
// pinned registry carries (they are all lower-case slugs) and the local names the
// registry does not.
var providerName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// New returns the engine that owns every guarantee of this port over one
// provider.
//
// It validates at composition and touches nothing: no dial, no count, no span. A
// configuration that cannot be honoured is refused here rather than discovered on
// the first ask, so an installation that cannot reach its endpoint at all still
// boots, and says so on the first call, as every other adapter here does.
func New(cfg Config, provider Provider) (Engine, error) {
	if provider == nil {
		return nil, errors.New("inference: an engine with no provider has no wire to speak on")
	}
	cfg, err := cfg.Defaults()
	if err != nil {
		return nil, err
	}
	if cfg.DailyCalls <= 0 {
		return nil, errors.New("inference: a provider requires a positive tenant allowance")
	}
	return &engine{cfg: cfg, provider: provider}, nil
}

type engine struct {
	cfg      Config
	provider Provider
	closed   atomic.Bool
}

func (e *engine) Model() string { return e.cfg.Model }

// Close releases the adapter's connections. A second Close, and every Chat after
// the first, answers ErrClosed: an engine that kept dialling after its process
// started releasing resources leaves an idle pool behind, and nothing reads it.
func (e *engine) Close() error {
	if !e.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	return e.provider.Close()
}

// Chat spends the allowance, asks the provider, and returns the answer only if it
// arrived whole. The order is the whole of the design:
//
//   - an ask that cannot be asked is refused before the counter is touched,
//     because a call that was never allowed to be made spends no allowance and a
//     call that will not be made is not measured;
//   - the allowance is spent before the wire, because a cap that dials first and
//     counts afterwards is a bill an exhausted tenant can still run up;
//   - the answer's size and completeness are checked after the provider and
//     before the caller, and a refusal there returns no text and stays counted —
//     the provider spent its tokens whether or not this installation can use them.
func (e *engine) Chat(ctx context.Context, ask Request) (Answer, error) {
	if e.closed.Load() {
		return Answer{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return Answer{}, err
	}
	if err := ask.shape(); err != nil {
		return Answer{}, err
	}
	_, ok := tenancy.FromContext(ctx)
	if !ok {
		return Answer{}, ErrNoTenant
	}
	if err := e.allow(ctx); err != nil {
		return Answer{}, err
	}
	ctx, span := telemetry.Tracer().Start(ctx, spanName(e.cfg.Model),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(e.requestAttrs(ctx)...))
	defer span.End()

	answer, err := e.ask(ctx, ask)
	if err != nil {
		// A class name and nothing else: the status description is the one field a
		// trace backend shows beside a name, and an error here can carry a
		// provider's sentence about the ask. No event is recorded on this span for
		// the same reason: see the package comment.
		span.SetStatus(codes.Error, classOf(err))
		span.SetAttributes(semconv.ErrorTypeKey.String(classOf(err)))
		return Answer{}, err
	}
	span.SetAttributes(e.answerAttrs(answer)...)
	return answer, nil
}

// spanName is the conventions' client-span shape, `{operation} {request model}`.
// The pinned semconv package carries the attribute key and the operation's value;
// the name reads that value rather than spelling it again, so a span and its
// attribute cannot disagree about which operation this was.
func spanName(model string) string {
	return semconv.GenAIOperationNameChat.Value.AsString() + " " + model
}

// ask is the wire, under the installation's own backstop deadline.
func (e *engine) ask(ctx context.Context, ask Request) (Answer, error) {
	callCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	resp, err := e.provider.Chat(callCtx, ask)
	if err != nil {
		return Answer{}, err
	}
	return e.honours(resp)
}

// allow reads the window, then spends it. Two reads, for the reason kit/limit
// gives for being readable: a limit with three answers rather than two has to be
// read before the attempt it is about, and the read is what lets this package name
// the reason correctly — ErrSpent, whose row says so, rather than
// ErrSpentUnproven, whose row was somebody else's.
func (e *engine) allow(ctx context.Context) error {
	if e.cfg.Counter == nil {
		return nil
	}
	n, left, err := e.cfg.Counter.Count(ctx, Bucket, e.cfg.Window)
	if err != nil {
		return errors.Join(ErrCounterUnavailable, err)
	}
	if n >= e.cfg.DailyCalls {
		return newSpent(ErrSpent, left)
	}
	ok, after, err := e.cfg.Counter.Allow(ctx, Bucket, e.cfg.DailyCalls, e.cfg.Window)
	if err != nil {
		return errors.Join(ErrCounterUnavailable, err)
	}
	if ok {
		return nil
	}
	if after >= e.cfg.Window {
		// The counter's queue: kit/limit answers a row it could not lock with the
		// whole window and no error, which is the figure that cannot understate the
		// wait. Nothing reached the row, so nothing about the window is known.
		return newSpent(ErrSpentUnproven, after)
	}
	return newSpent(ErrSpent, after)
}

// honours reads the provider's answer against this installation's two promises —
// that what a caller gets is whole and that it fits — and returns the answer only
// when both hold.
func (e *engine) honours(resp Response) (Answer, error) {
	if resp.Truncated || (resp.Finish != "" && resp.Finish != "stop") {
		return Answer{}, ErrAnswerIncomplete
	}
	if resp.Text == "" {
		return Answer{}, ErrAnswerEmpty
	}
	if len(resp.Text) > e.cfg.MaxAnswerBytes {
		return Answer{}, fmt.Errorf("%w: %d bytes read against a limit of %d", ErrAnswerTooLarge, len(resp.Text), e.cfg.MaxAnswerBytes)
	}
	if !resp.Usage.Reported || resp.Usage.InputTokens < 0 || resp.Usage.OutputTokens < 0 {
		return Answer{}, ErrAnswerShape
	}
	return Answer{Text: resp.Text, Model: resp.Model, Usage: resp.Usage}, nil
}

// requestAttrs is what a span carries about an ask, which is everything except
// the ask: the operation, the provider, the model, and the tenant and request this
// kernel stamps on every span it opens. No prompt, no instruction, no image byte —
// see the package comment.
func (e *engine) requestAttrs(ctx context.Context) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		semconv.GenAIOperationNameChat,
		semconv.GenAIProviderNameKey.String(e.cfg.Provider),
		semconv.GenAIRequestModel(e.cfg.Model),
	}
	return append(attrs, telemetry.SpanAttrs(ctx)...)
}

// answerAttrs records reported counts and a response model only when it matches
// the configured name. Arbitrary response metadata can echo private ask content.
func (e *engine) answerAttrs(a Answer) []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	if a.Model == e.cfg.Model {
		attrs = append(attrs, semconv.GenAIResponseModel(a.Model))
	}
	if a.Usage.Reported {
		attrs = append(attrs,
			semconv.GenAIUsageInputTokens(a.Usage.InputTokens),
			semconv.GenAIUsageOutputTokens(a.Usage.OutputTokens),
		)
	}
	return attrs
}

// shape is the ask refused by name, in the order a caller can act on: the missing
// question first, then the three byte bounds, then the picture's encoding, which
// is the one check that reads what it was sent rather than its size.
func (a Request) shape() error {
	if a.Prompt == "" {
		return errors.Join(ErrAskShape, fault.ErrInvalid)
	}
	if len(a.Prompt) > MaxPromptBytes {
		return fmt.Errorf("%w: the prompt is %d bytes against a limit of %d", ErrAskTooLarge, len(a.Prompt), MaxPromptBytes)
	}
	if len(a.Instruction) > MaxInstructionBytes {
		return fmt.Errorf("%w: the instruction is %d bytes against a limit of %d", ErrAskTooLarge, len(a.Instruction), MaxInstructionBytes)
	}
	if len(a.Image) > MaxImageBytes {
		return fmt.Errorf("%w: the picture is %d bytes against a limit of %d", ErrImageTooLarge, len(a.Image), MaxImageBytes)
	}
	if len(a.Image) > 0 && !isPNG(a.Image) {
		return ErrImageFormat
	}
	return nil
}

// isPNG answers a picture's encoding from its signature and its header, without
// decoding a pixel: cheap enough to run on every ask, and enough to refuse a
// JPEG, a PDF or a text file claiming to be a PNG.
func isPNG(b []byte) bool {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	return err == nil && cfg.Width > 0 && cfg.Height > 0
}

// classOf is the closed set this package's failures arrive under in telemetry.
// It answers a class and never a sentence: a status description that carries a
// provider's own words carries whatever the provider read out of the ask.
func classOf(err error) string {
	switch {
	case errors.Is(err, ErrNoTenant):
		return "no_tenant"
	case errors.Is(err, ErrAskShape):
		return "ask_shape"
	case errors.Is(err, ErrAskTooLarge):
		return "ask_too_large"
	case errors.Is(err, ErrImageTooLarge):
		return "image_too_large"
	case errors.Is(err, ErrImageFormat):
		return "image_format"
	case errors.Is(err, ErrSpentUnproven):
		return "allowance_unproven"
	case errors.Is(err, ErrSpent):
		return "allowance_spent"
	case errors.Is(err, ErrCounterUnavailable):
		return "counter_unavailable"
	case errors.Is(err, ErrAnswerEmpty):
		return "answer_empty"
	case errors.Is(err, ErrAnswerShape):
		return "answer_shape"
	case errors.Is(err, ErrAnswerIncomplete):
		return "answer_incomplete"
	case errors.Is(err, ErrAnswerTooLarge):
		return "answer_size"
	case errors.Is(err, ErrResponseTooLarge):
		return "response_too_large"
	case errors.Is(err, ErrProviderRefused):
		return "provider_refused"
	case errors.Is(err, ErrProviderUnavailable):
		return "provider_unreachable"
	case errors.Is(err, ErrNoScript):
		return "not_configured"
	case errors.Is(err, ErrClosed):
		return "closed"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "unknown"
	}
}

// StatusFor is the HTTP verdict a door answers each refusal with. It is exported
// for that one caller, and it answers within the closed set kit/telemetry already
// keeps: 429 for a refusal that says when to come back, 503 for a thing that could
// not be counted, and 422 for an ask its sender can correct.
func StatusFor(err error) int {
	switch {
	case errors.Is(err, ErrSpent), errors.Is(err, ErrSpentUnproven):
		return http.StatusTooManyRequests
	case errors.Is(err, ErrCounterUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrAskShape):
		return http.StatusUnprocessableEntity
	case errors.Is(err, ErrAskTooLarge), errors.Is(err, ErrImageTooLarge), errors.Is(err, ErrImageFormat):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrAnswerShape), errors.Is(err, ErrAnswerTooLarge), errors.Is(err, ErrResponseTooLarge):
		return http.StatusInternalServerError
	case errors.Is(err, ErrAnswerEmpty), errors.Is(err, ErrAnswerIncomplete):
		return http.StatusBadGateway
	case errors.Is(err, ErrProviderRefused):
		return http.StatusBadGateway
	case errors.Is(err, ErrProviderUnavailable):
		return http.StatusServiceUnavailable
	case errors.Is(err, ErrNoTenant), errors.Is(err, ErrClosed), errors.Is(err, ErrNoScript):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
