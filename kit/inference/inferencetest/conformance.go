// Package inferencetest is the one suite every adapter of inference.Engine runs.
//
// Both far ends in this repository call it — the script in kit/inference's own
// test, the HTTP adapter over a stub server in the same file — for the reason
// kit/cache/cachetest exists: an adapter that never runs the suite is an adapter
// nobody tested, and a port with one implementation has no portability claim to
// make. Every case says what a wrong adapter would do, so the suite cannot be
// passed by an implementation that quietly means something else.
//
// # The rule that makes it one suite
//
// A case says "the far end must recover these exact bytes" and each leg says how.
// The script recovers them from the Request it was handed; the HTTP leg
// base64-decodes the data URL out of the JSON its handler received. An adapter
// that posted raw PNG bytes, or built a data URL with the wrong MIME prefix,
// passes on the script and fails on the wire — which is the difference the suite
// exists to find, and no amount of reading the adapter's own file would.
//
// # The three tiers, and who can play them
//
// A leg declares which tiers it Honours. Port semantics are what every far end
// must do; far-end deviations need an endpoint that misbehaves on cue, which a
// hosted vendor will not do for a test; the counter store is a leg whose Counter is
// the real row. A leg that declines a tier prints that it declined, and a leg that
// cannot answer the counter cases fails rather than skips: a suite that quietly
// skipped the store it ships proves nothing.
package inferencetest

import (
	"bytes"
	"context"
	"errors"
	"image"
	"net/http"
	"strings"
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

	// C-1. An ask with no question is refused and the far end sees nothing. A wrong
	// adapter counts the attempt, dials, and returns the vendor's 400 as if it were
	// an outage — a person's empty form costs the tenant a call.
	t.Run("an ask with no question reaches no wire", func(t *testing.T) {
		engine, _, seen := open(t, fresh, End{})
		if _, err := engine.Chat(askCtx(t), inference.Request{Instruction: "name what is drawn"}); !errors.Is(err, inference.ErrAskShape) {
			t.Errorf("Chat(empty prompt) = %v, want ErrAskShape", err)
		}
		if got := seen.Asked(t); got != 0 {
			t.Errorf("the far end saw %d ask(s); a refused ask was sent, and counted", got)
		}
	})

	// C-2. A text-only ask round-trips: the text, the model name and the usage the
	// endpoint reported. A wrong adapter loses the model name, and then no span can
	// tell two endpoints apart.
	t.Run("a text-only ask round-trips", func(t *testing.T) {
		deviates(t, leg)
		engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Model: "far-model",
			Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if answer.Text != "a cat" {
			t.Errorf("answer %q, want the text the far end said", answer.Text)
		}
		if answer.Model != "far-model" {
			t.Errorf("model %q, want the name the far end gave it", answer.Model)
		}
		if !answer.Usage.Reported || answer.Usage.InputTokens != 7 || answer.Usage.OutputTokens != 3 {
			t.Errorf("usage %+v, want the counts the far end reported", answer.Usage)
		}
		sent := seen.Sent(t)
		if len(sent) != 1 || sent[0].Prompt != "what is this?" {
			t.Fatalf("the far end saw %+v, want the one prompt", sent)
		}
	})

	// C-3. A picture arrives as one image part whose decoded bytes are byte-identical,
	// in an ask of exactly two content parts. A wrong adapter posts raw PNG bytes as
	// text, names the data URL's wrong MIME type, or puts the picture in the system
	// role — every one of which passes a unit test over the Go value.
	t.Run("a picture arrives as the bytes it was given", func(t *testing.T) {
		engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		if _, err := engine.Chat(askCtx(t), picture); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		sent := seen.Sent(t)
		if len(sent) != 1 {
			t.Fatalf("the far end saw %d ask(s), want one", len(sent))
		}
		if string(sent[0].Image) != string(picture.Image) {
			t.Errorf("the far end recovered %d bytes, not the %d it was sent", len(sent[0].Image), len(picture.Image))
		}
		if sent[0].Parts != 2 {
			t.Errorf("the far end saw %d content part(s); an ask with a picture is a text part and an image part", sent[0].Parts)
		}
		if sent[0].Instruction != picture.Instruction {
			t.Errorf("the steer arrived as %q; a steer that does not arrive is a rule nobody sent", sent[0].Instruction)
		}
	})

	// C-4. Bytes that are not a PNG are refused by name and nothing is sent. A wrong
	// adapter forwards them and takes the vendor's 400 as an outage, which turns a
	// person's unsupported upload into a retry.
	t.Run("bytes that are not a PNG are refused by name", func(t *testing.T) {
		engine, _, seen := open(t, fresh, End{})
		jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, []byte("not a png, only a claim of one")...)
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: jpeg}); !errors.Is(err, inference.ErrImageFormat) {
			t.Errorf("Chat(jpeg) = %v, want ErrImageFormat", err)
		}
		if got := seen.Asked(t); got != 0 {
			t.Errorf("the far end saw %d ask(s); an ask this kit refuses must not be paid for", got)
		}
	})

	// C-5. A picture at the bound is refused and one byte under it is sent. The
	// wrong adapter is an off-by-one that drops real pictures, or a bound nobody
	// read at all.
	t.Run("the picture bound refuses the byte over and sends the byte under", func(t *testing.T) {
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "ok", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		over := samplePNG(1, 1)
		over = append(append([]byte(nil), over...), make([]byte, inference.MaxImageBytes-len(over)+1)...)
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: over}); !errors.Is(err, inference.ErrImageTooLarge) {
			t.Errorf("Chat(over the bound) = %v, want ErrImageTooLarge", err)
		}
		under := make([]byte, inference.MaxImageBytes-1)
		copy(under, samplePNG(1, 1))
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: under}); err != nil &&
			!errors.Is(err, inference.ErrProviderUnavailable) && !errors.Is(err, inference.ErrAnswerEmpty) {
			t.Errorf("Chat(one byte under the bound) = %v; a bound nobody read is a bound that refuses real pictures", err)
		}
	})

	// C-6. A prompt over the bound is refused before the counter. A wrong adapter
	// bills a tenant for a document and reads the whole body of the answer it gets
	// back for it.
	t.Run("a prompt that is a document is refused before the counter", func(t *testing.T) {
		engine, store, _ := open(t, fresh, End{})
		long := make([]byte, inference.MaxPromptBytes+1)
		for i := range long {
			long[i] = 'x'
		}
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: string(long)}); !errors.Is(err, inference.ErrAskTooLarge) {
			t.Errorf("Chat(long prompt) = %v, want ErrAskTooLarge", err)
		}
		if store.reads != 0 {
			t.Errorf("the allowance was read %d time(s) for an ask that was never allowed to be made", store.reads)
		}
	})

	// C-7. An answer of exactly the bound is returned; one byte more is refused and
	// returns no text at all. A wrong adapter truncates happily and stores half a
	// translation, which is the data corruption no caller can detect afterwards.
	t.Run("an answer over the bound returns no text", func(t *testing.T) {
		deviates(t, leg)
		long := make([]byte, 65)
		for i := range long {
			long[i] = 'x'
		}
		for _, tc := range []struct {
			text    string
			wantErr bool
		}{{string(long[:64]), false}, {string(long), true}} {
			engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: tc.text, Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
			answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
			if tc.wantErr && !errors.Is(err, inference.ErrAnswerTooLarge) {
				t.Errorf("Chat(answer %d bytes) = %v, want ErrAnswerTooLarge", len(tc.text), err)
			}
			if tc.wantErr && answer.Text != "" {
				t.Errorf("a refused answer still carried %d bytes of text", len(answer.Text))
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Chat(answer at the bound) = %v, want it returned", err)
			}
		}
	})

	// C-8. A far end that says it cut the answer short is refused whole-and-nothing.
	// A wrong adapter hands back a truncated guess as if the model had finished, and
	// the caller has nothing to detect that with.
	t.Run("an answer the endpoint cut short is refused", func(t *testing.T) {
		deviates(t, leg)
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat sit", Finish: "length"}}})
		answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		if !errors.Is(err, inference.ErrAnswerIncomplete) {
			t.Errorf("Chat(finish=length) = %v, want ErrAnswerIncomplete", err)
		}
		if answer.Text != "" {
			t.Errorf("a truncated answer was returned as %q", answer.Text)
		}
	})

	// C-9. A 200 with nothing in it is refused. A wrong adapter returns "" with a nil
	// error, which a caller reads as "the model said nothing" — the honest-looking
	// wrong answer, and the one a person cannot tell from an empty drawing.
	t.Run("an answer of nothing is refused", func(t *testing.T) {
		deviates(t, leg)
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: ""}}})
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrAnswerEmpty) {
			t.Errorf("Chat(no answer) = %v, want ErrAnswerEmpty", err)
		}
	})

	// C-10. Missing usage refuses the whole answer; no token count is invented.
	t.Run("an answer without reported usage is refused", func(t *testing.T) {
		deviates(t, leg)
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat"}}})
		answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		if !errors.Is(err, inference.ErrAnswerShape) || answer != (inference.Answer{}) {
			t.Errorf("unaccounted answer = %+v, %v; want no answer and ErrAnswerShape", answer, err)
		}
	})

	// C-11. A deadline in the context ends the call inside itself. A wrong adapter
	// keeps a goroutine and a connection after the caller gave up, which is a leak
	// with a tenant's name on it.
	t.Run("a deadline ends the call inside itself", func(t *testing.T) {
		deviates(t, leg)
		engine, _, _ := open(t, fresh, End{Silence: 5 * time.Second})
		when, cancel := context.WithTimeout(askCtx(t), 150*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := engine.Chat(when, inference.Request{Prompt: "what is this?"})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Chat(deadline) = %v, want context.DeadlineExceeded", err)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("the call returned %s after its deadline; a caller that gave up should not be kept", time.Since(start))
		}
	})

	// C-12. Cancelling mid-call returns promptly, and the allowance stays spent:
	// the far end served the call. A wrong adapter refunds a spend the provider
	// already paid for, or ignores its cancellation until the answer lands.
	t.Run("a cancelled call returns promptly and stays spent", func(t *testing.T) {
		deviates(t, leg)
		store := &countedCounter{}
		engine, store, _ := open(t, fresh, End{Silence: 5 * time.Second})
		givingUp, cancel := context.WithCancel(askCtx(t))
		go func() {
			time.Sleep(150 * time.Millisecond)
			cancel()
		}()
		if _, err := engine.Chat(givingUp, inference.Request{Prompt: "what is this?"}); !errors.Is(err, context.Canceled) {
			t.Errorf("Chat(cancel) = %v, want context.Canceled", err)
		}
		cancel()
		n, _, err := store.Count(askCtx(t), inference.Bucket, time.Hour)
		if err != nil {
			t.Fatalf("counting what the cancelled call spent: %v", err)
		}
		if n != 1 {
			t.Errorf("the allowance holds %d, want 1: the far end served that call, whoever asked it to stop", n)
		}
	})

	// C-15. The Nth ask is allowed and the N+1th refused with a RetryAfter inside the
	// window, and the far end saw exactly N. A wrong adapter dials first and counts
	// afterwards, so exhaustion costs money every time it is reached.
	t.Run("the allowance is spent before the wire", func(t *testing.T) {
		engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		for range 3 {
			if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); err != nil {
				t.Fatalf("the third ask should still be allowed: %v", err)
			}
		}
		_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		spent, ok := errors.AsType[*inference.SpentError](err)
		if !ok || !errors.Is(err, inference.ErrSpent) {
			t.Fatalf("Chat(beyond the allowance) = %v, want a *SpentError over ErrSpent", err)
		}
		if spent.RetryAfter <= 0 || spent.RetryAfter > time.Hour {
			t.Errorf("RetryAfter %s, want inside the window", spent.RetryAfter)
		}
		// The refusal has to say what it is twice over: a number the caller can
		// wait on, and the class and status a door counts and answers without
		// reading a sentence. A refusal that arrives as an anonymous error is a 500
		// with the tenant's own allowance written on it.
		if got := inference.StatusFor(err); got != http.StatusTooManyRequests {
			t.Errorf("a spent allowance answers %d, want 429", got)
		}
		if spent.Class() != "allowance_spent" {
			t.Errorf("a spent allowance is counted as %q, want allowance_spent", spent.Class())
		}
		if got := seen.Asked(t); got != 3 {
			t.Errorf("the far end saw %d ask(s), want the 3 the allowance covered", got)
		}
	})

	// C-16. One tenant's spent allowance does not touch another's. The wrong adapter
	// builds a key from caller data or forgets the tenant segment, which is the same
	// argument kit/limit's scoping and cache's fixed-width prefix make.
	t.Run("two tenants spend two allowances", func(t *testing.T) {
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		for range 3 {
			if _, err := engine.Chat(tenancy.WithTenant(askCtx(t), tenantA), inference.Request{Prompt: "what is this?"}); err != nil {
				t.Fatalf("tenant alpha's third ask: %v", err)
			}
		}
		if _, err := engine.Chat(tenancy.WithTenant(askCtx(t), tenantB), inference.Request{Prompt: "what is this?"}); err != nil {
			t.Errorf("tenant beta was refused after alpha spent its own allowance: %v", err)
		}
	})

	// C-17. An ask with no resolved tenant is refused, nothing counted and nothing
	// sent. A wrong adapter counts under the nil UUID, so one tenant can lock the
	// installation's one shared bucket and refuse strangers.
	t.Run("an ask with no tenant is refused before the counter", func(t *testing.T) {
		store := &countedCounter{}
		engine, store, seen := open(t, fresh, End{})
		_, err := engine.Chat(context.Background(), inference.Request{Prompt: "what is this?"})
		if !errors.Is(err, inference.ErrNoTenant) {
			t.Errorf("Chat(no tenant) = %v, want ErrNoTenant", err)
		}
		if got := inference.StatusFor(err); got != http.StatusInternalServerError {
			t.Errorf("an ask with no tenant answers %d; it is the caller's own bug, not a refusal of a person", got)
		}
		if store.reads != 0 {
			t.Errorf("the allowance was read %d time(s) for an ask with no tenant to charge", store.reads)
		}
		if got := seen.Asked(t); got != 0 {
			t.Errorf("the far end saw %d ask(s); a tenant-less ask must never reach a vendor", got)
		}
	})

	// C-18. A counter that cannot be reached refuses, and the far end sees nothing.
	// The wrong adapter fails open on the error, which is the failure mode
	// kit/limit/README.md exists to stop: an uncounted model call is a bill nobody
	// authorised.
	t.Run("a counter that cannot be reached refuses", func(t *testing.T) {
		if leg.Uncountable == nil {
			t.Fatalf("%s names no uncountable leg; what its allowance does during an outage is a case, not a comment", leg.Name)
		}
		engine, _, seen := open(t, fresh, End{}, withCounter(leg.Uncountable(t)))
		_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		if !errors.Is(err, inference.ErrCounterUnavailable) {
			t.Errorf("Chat(counter down) = %v, want ErrCounterUnavailable", err)
		}
		// Fail closed means the door says "come back", never "here is your answer":
		// a 200 on an uncountable allowance is the outage the cap exists to refuse.
		if got := inference.StatusFor(err); got != http.StatusServiceUnavailable {
			t.Errorf("an allowance that cannot be counted answers %d, want 503", got)
		}
		if got := seen.Asked(t); got != 0 {
			t.Errorf("the far end saw %d ask(s) while the allowance could not be counted", got)
		}
	})

	// C-20. Every ask that reached the far end is counted, including the answers
	// refused after the wire. A wrong adapter refunds a spend the provider already
	// paid for, which makes a chatty endpoint free to hammer.
	t.Run("every ask that reached the far end is counted", func(t *testing.T) {
		deviates(t, leg)
		store := &countedCounter{}
		engine, store, seen := open(t, fresh, End{Answers: []inference.Response{{Text: strings.Repeat("a cat ", 20)}}})
		for range 3 {
			if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrAnswerTooLarge) {
				t.Fatalf("every ask should be refused for size here, and was not: %v", err)
			}
		}
		n, _, err := store.Count(askCtx(t), inference.Bucket, time.Hour)
		if err != nil {
			t.Fatalf("counting: %v", err)
		}
		if got := seen.Asked(t); n != got {
			t.Errorf("the allowance holds %d and the far end served %d; a call that cost is a call that counts", n, got)
		}
	})

	// C-21. Model names the configured model, and an engine with no endpoint names
	// practice. A wrong application cannot tell a real model from the fake, which is
	// "it never pretends" broken.
	t.Run("an engine names the model it speaks to", func(t *testing.T) {
		engine, _, configured := open(t, fresh, End{})
		if engine.Model() != configured.Model {
			t.Errorf("Model() = %q, want the configured model", engine.Model())
		}
	})
	t.Run("an engine with no endpoint says so", func(t *testing.T) {
		if inference.Practice().Model() != inference.PracticeModel {
			t.Errorf("Practice().Model() = %q, want %q", inference.Practice().Model(), inference.PracticeModel)
		}
	})

	// C-22. After Close, Chat answers ErrClosed and the far end sees nothing; the
	// second Close answers ErrClosed. A wrong adapter leaves an idle pool behind and
	// keeps spending an allowance nobody is asking about.
	t.Run("a closed engine asks nothing", func(t *testing.T) {
		engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); err != nil {
			t.Fatalf("the ask before Close: %v", err)
		}
		if err := engine.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrClosed) {
			t.Errorf("Chat after Close = %v, want ErrClosed", err)
		}
		if err := engine.Close(); !errors.Is(err, inference.ErrClosed) {
			t.Errorf("the second Close = %v, want ErrClosed", err)
		}
		if got := seen.Asked(t); got != 1 {
			t.Errorf("the far end saw %d ask(s), want the one before Close", got)
		}
	})

	// C-23. N concurrent asks at a cap of 1 put one ask on the wire. The wrong
	// adapter reads the same count in two goroutines, dials twice, and finds the cap
	// was a suggestion. Run under -race.
	t.Run("concurrent asks do not both pass one allowance", func(t *testing.T) {
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}}, calls(1))
		var wg sync.WaitGroup
		allowed := make(chan error, 8)
		for range 8 {
			wg.Go(func() {
				_, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
				allowed <- err
			})
		}
		wg.Wait()
		close(allowed)
		sent := 0
		for err := range allowed {
			if err == nil {
				sent++
			}
		}
		if sent != 1 {
			t.Errorf("%d of 8 concurrent asks were answered, at an allowance of 1", sent)
		}
	})

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
