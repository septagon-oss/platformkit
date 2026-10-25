// Package inferencetest supplies the shared inference.Engine conformance cases.
// Each leg recovers requests at its own boundary, including decoded HTTP image
// bytes. Legs declare supported tiers; unsupported endpoint deviations name their
// skip, while a missing counter implementation fails the allowance cases.
package inferencetest

import (
	"bytes"
	"context"
	"image"
	"sync"
	"testing"
	"time"

	png "image/png"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Tier is one of the three groups of cases, and the thing a leg declares it can
// play.
type Tier int

const (
	// PortSemantics is what the port promises whatever the far end is: an ask,
	// bounds, an answer, a spend, a refusal.
	PortSemantics Tier = iota
	// FarEndDeviation is an endpoint that misbehaves on cue. Only a far end under
	// test's control can play it.
	FarEndDeviation
	// CounterStore is the allowance kept in the store every replica reads.
	CounterStore
)

// End is what a case asks the far end to do.
type End struct {
	// Answers are returned in order; the last one answers every later ask.
	Answers []inference.Response
	// Silence is how long the far end waits before answering — the shape of an
	// endpoint that never will.
	Silence time.Duration
	// Err is the transport-level failure this leg can produce. The bodies a raw
	// endpoint answers — too large, not json, a prompt echoed back inside an error —
	// belong to the leg that has a response to read, and the OpenAI adapter's own
	// cases hold them: a fake with no transport cannot answer a body it has no shape
	// for, and a suite field no case fills is a promise nobody checks.
	Err error
}

// Seen is what the far end recovered from what the adapter sent: enough to prove
// the promise reached the wire rather than stayed in the Go value.
type Seen struct {
	Instruction string
	Prompt      string
	// Image is the picture as it arrived, after undoing whatever encoding that
	// wire used. A leg that never carried one leaves it nil.
	Image []byte
	// Parts is how many content parts the far end saw: a text-only ask is one, a
	// picture ask two, and the difference is what an endpoint refuses on.
	Parts int
}

// Leg is one adapter under test.
type Leg struct {
	Name string
	// Model overrides the suite model for an operator-configured endpoint.
	// Empty means "conformance". Build must honour this configured name.
	Model string
	// Build returns an engine whose far end does what end says, over cfg. The leg
	// is expected to honour cfg — its bounds, its model, its counter — and the
	// cases assert that it did rather than trusting the constructor.
	Build func(t *testing.T, end End, cfg inference.Config) inference.Engine
	// Counter names the allowance store this leg rations on. It is the leg's
	// because only the leg knows how its store is reached; the suite wraps it, so
	// what a case asserts is the count the store holds and not the count this
	// package kept.
	Counter func(t *testing.T) inference.Counter
	// Sent returns what the far end recovered, in order.
	Sent func(t *testing.T) []Seen
	// Asked is how many asks reached the far end — the number a refusal must
	// not have paid for.
	Asked func(t *testing.T) int
	// Uncountable returns the counter this leg cannot reach: no pool, no process, a
	// store that refused. The suite puts it on an engine over this leg's own far
	// end, so the case can assert the wire saw nothing. A leg without one is a leg
	// that has not answered what its allowance does during an outage, so it fails.
	Uncountable func(t *testing.T) inference.Counter
	// Total is what this leg cost in asks across every engine it built — the
	// figures the suite prints and refuses a runaway against. Nil prints nothing.
	Total func(t *testing.T) int
	// Honours answers whether this far end can play a tier. Nil means every one.
	Honours func(tier Tier) bool
}

// Legs are the far ends the suite runs, one factory each. A factory is asked per
// case, because a Sent or an Asked that outlived its own case would tell the next
// case a story about the last one: what a case asserts is what *its* engine sent.
type Legs func(t *testing.T) Leg

func (l Leg) plays(tier Tier) bool {
	return l.Honours == nil || l.Honours(tier)
}

// The two tenants of every case, fixed bytes rather than fresh uuids so a key that
// lets one reach the other is reproducible from the failure message alone.
var (
	tenantA = tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "alpha"}
	tenantB = tenancy.Tenant{ID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), Slug: "beta"}
)

// picture is what every picture case sends: four bytes of PNG header plus the
// signature, so a leg that base64s it and a leg that decodes it agree on length,
// and the bytes are a real PNG a decoder accepts.
var picture = inference.Request{Instruction: "name what is drawn", Prompt: "what is this?", Image: samplePNG(2, 2)}

// png returns a w-by-h PNG — a real one, encoded, because a case that hand-wrote a
// header would test the signature check and nothing else.
func samplePNG(w, h int) []byte {
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return out.Bytes()
}

// Config is the configuration every case runs a leg under, with the bounds the
// port states and an allowance the case can exhaust in three calls.
func Config(model string, counter inference.Counter) inference.Config {
	return inference.Config{
		Model:            model,
		DailyCalls:       3,
		Window:           time.Hour,
		MaxAnswerBytes:   64,
		MaxResponseBytes: 4096,
		Timeout:          5 * time.Second,
		Counter:          counter,
	}
}

// Conformance runs the suite over each leg in turn, and prints per leg what it cost.
func Conformance(t *testing.T, legs ...Legs) {
	t.Helper()
	for _, leg := range legs {
		t.Run(leg(t).Name, func(t *testing.T) { run(t, leg) })
	}
}

// countedCounter is the store a case reads: the leg's own counting, with what this
// package spent on the case beside it, so a refusal cannot spend a unit nobody
// asked the store about.
type countedCounter struct {
	inner inference.Counter
	mu    sync.Mutex
	reads int
}

func (c *countedCounter) Count(ctx context.Context, key string, window time.Duration) (int, time.Duration, error) {
	c.mu.Lock()
	c.reads++
	c.mu.Unlock()
	return c.inner.Count(ctx, key, window)
}

func (c *countedCounter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	return c.inner.Allow(ctx, key, limit, window)
}

// run is the cases. Each opens with what a wrong adapter does, so a green run is
// evidence rather than a shape that happens to pass.
func run(t *testing.T, fresh Legs) {
	t.Helper()
	leg := fresh(t)
	if leg.Build == nil || leg.Sent == nil || leg.Asked == nil || leg.Counter == nil {
		t.Fatalf("%s is not a leg: Build, Counter, Sent and Asked are what a leg owes the suite", leg.Name)
	}
	if leg.Total != nil {
		t.Cleanup(func() {
			total := leg.Total(t)
			t.Logf("%s: %d ask(s) reached the far end across the suite", leg.Name, total)
			if total > 24 {
				t.Errorf("%s spent %d asks of the far end across the suite, past the 24 a leg may cost. A suite "+
					"whose cap cases dial sixty times is a bill, not a test.", leg.Name, total)
			}
		})
	}

	t.Run("an ask with no question reaches no wire", func(t *testing.T) { emptyAsk(t, fresh) })
	t.Run("a text-only ask round-trips", func(t *testing.T) { textAnswer(t, fresh, leg) })
	t.Run("a picture arrives as the bytes it was given", func(t *testing.T) { pictureBytes(t, fresh) })
	t.Run("bytes that are not a PNG are refused by name", func(t *testing.T) { imageFormat(t, fresh) })
	t.Run("the picture bound refuses the byte over and sends the byte under", func(t *testing.T) { imageBound(t, fresh) })
	t.Run("a prompt that is a document is refused before the counter", func(t *testing.T) { promptBound(t, fresh) })
	t.Run("an answer over the bound returns no text", func(t *testing.T) { answerBound(t, fresh, leg) })
	t.Run("an answer the endpoint cut short is refused", func(t *testing.T) { incompleteAnswer(t, fresh, leg) })
	t.Run("an answer of nothing is refused", func(t *testing.T) { emptyAnswer(t, fresh, leg) })
	t.Run("an answer without reported usage is refused", func(t *testing.T) { reportedUsage(t, fresh, leg) })
	t.Run("a deadline ends the call inside itself", func(t *testing.T) { deadline(t, fresh, leg) })
	t.Run("a cancelled call returns promptly and stays spent", func(t *testing.T) { cancelledCall(t, fresh, leg) })
	t.Run("the allowance is spent before the wire", func(t *testing.T) { allowanceBeforeWire(t, fresh) })
	t.Run("two tenants spend two allowances", func(t *testing.T) { tenantAllowances(t, fresh) })
	t.Run("an ask with no tenant is refused before the counter", func(t *testing.T) { requireTenant(t, fresh) })
	t.Run("a counter that cannot be reached refuses", func(t *testing.T) { unavailableCounter(t, fresh, leg) })
	t.Run("every ask that reached the far end is counted", func(t *testing.T) { countEveryAsk(t, fresh, leg) })
	t.Run("an engine names the model it speaks to", func(t *testing.T) { modelName(t, fresh) })
	t.Run("an engine with no endpoint says so", func(t *testing.T) { practiceName(t) })
	t.Run("a closed engine asks nothing", func(t *testing.T) { closedEngine(t, fresh) })
	t.Run("concurrent asks do not both pass one allowance", func(t *testing.T) { concurrentAllowance(t, fresh) })
}

// open builds a leg's engine over the store the case will read back, so what a case
// asserts is the count the store holds and not the count this package kept. tune is
// the case's own edit of the configuration — a smaller allowance, a shorter bound.
func open(t *testing.T, fresh Legs, end End, tune ...func(*inference.Config)) (inference.Engine, *countedCounter, Leg) {
	t.Helper()
	leg := fresh(t)
	store := &countedCounter{inner: leg.Counter(t)}
	if leg.Model == "" {
		leg.Model = "conformance"
	}
	cfg := Config(leg.Model, store)
	for _, edit := range tune {
		edit(&cfg)
	}
	return leg.Build(t, end, cfg), store, leg
}

// deviates skips a case whose far end has to misbehave on cue. A hosted endpoint
// will not answer a 37-byte truncated reply to order, and a script has no transport
// to break; the skip says which of those it is rather than pretending the case ran.
func deviates(t *testing.T, leg Leg) {
	t.Helper()
	if !leg.plays(FarEndDeviation) {
		t.Skipf("%s cannot make its far end misbehave, and this case needs one that does", leg.Name)
	}
}

// calls is the allowance a case wants to run under.
func calls(n int) func(*inference.Config) {
	return func(c *inference.Config) { c.DailyCalls = n }
}

// withCounter is the store a case wants instead of the leg's own.
func withCounter(c inference.Counter) func(*inference.Config) {
	return func(cfg *inference.Config) { cfg.Counter = c }
}

// askCtx is a context with a tenant in it, which is what every ask needs and what a
// bare context.Background is not.
func askCtx(t *testing.T) context.Context {
	t.Helper()
	return tenancy.WithTenant(t.Context(), tenantA)
}
