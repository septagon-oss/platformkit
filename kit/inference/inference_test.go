package inference_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	png "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/inferencetest"
	"github.com/septagon-oss/platformkit/kit/inference/providers/openai"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestEveryAdapterRunsTheOneSuite runs the port's conformance suite over both far
// ends this repository ships: the script every consumer's test will run on, and the
// OpenAI-compatible adapter over a loopback stub.
//
// Two legs, one suite, is the whole claim of portability this package makes. A run
// over the script alone would prove the engine's rules and nothing about the wire;
// the provider legs a deployment actually dials are named by
// `make inference-conformance`, and the PR that runs them names them in its
// Verified paragraph.
// What each far end cost across every engine the suite built: the figure a leg may
// not exceed, and the one a person reading a green run cannot otherwise see.
var (
	scriptTotal int
	openaiTotal int
)

func TestEveryAdapterRunsTheOneSuite(t *testing.T) {
	inferencetest.Conformance(t, scriptLeg, openaiLeg)
}

// The tenant of every ask below, and the one thing a bare context lacks.
var tenant = tenancy.Tenant{ID: uuid.MustParse("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"), Slug: "asking"}

// scriptLeg is the far end a consumer's test runs on: a script, an ask log, and no
// network at all.
func scriptLeg(t *testing.T) inferencetest.Leg {
	var mu sync.Mutex
	var far *inference.Script
	var asked int
	return inferencetest.Leg{
		Name: "the in-process engine",
		Build: func(t *testing.T, end inferencetest.End, cfg inference.Config) inference.Engine {
			replies := make([]inference.Reply, 0, len(end.Answers))
			for _, answer := range end.Answers {
				replies = append(replies, inference.Reply{Text: answer.Text, Model: answer.Model,
					Finish: answer.Finish, Usage: answer.Usage, Err: end.Err})
			}
			if len(replies) == 0 {
				replies = append(replies, inference.Reply{Err: end.Err})
			}
			far = inference.NewScript(replies...)
			asked = 0
			engine, err := inference.New(cfg, &delayedScript{Script: far, silence: end.Silence,
				mu: &mu, asked: &asked})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { engine.Close() })
			return engine
		},
		Counter: func(t *testing.T) inference.Counter { return limit.Memory() },
		Sent: func(t *testing.T) []inferencetest.Seen {
			seen := far.Asks()
			out := make([]inferencetest.Seen, 0, len(seen))
			for _, ask := range seen {
				out = append(out, inferencetest.Seen{
					Instruction: ask.Instruction, Prompt: ask.Prompt, Image: ask.Image,
					Parts: boolToInt(ask.Prompt != "") + boolToInt(ask.Image != nil),
				})
			}
			return out
		},
		Asked:       func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return asked },
		Uncountable: func(t *testing.T) inference.Counter { return unreachable{} },
		Total:       func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return scriptTotal },
	}
}

// delayedScript measures calls and simulates latency around the shipped Script.
// Reply traversal, request recording and lifetime stay owned by Script.
type delayedScript struct {
	*inference.Script
	silence time.Duration
	mu      *sync.Mutex
	asked   *int
}

func (s *delayedScript) Chat(ctx context.Context, ask inference.Request) (inference.Response, error) {
	s.mu.Lock()
	*s.asked++
	scriptTotal++
	s.mu.Unlock()
	if s.silence > 0 {
		select {
		case <-time.After(s.silence):
		case <-ctx.Done():
			return inference.Response{}, ctx.Err()
		}
	}
	return s.Script.Chat(ctx, ask)
}

// openaiLeg is the adapter over a loopback stub: the same cases, judged on the bytes
// that left the process.
func openaiLeg(t *testing.T) inferencetest.Leg {
	var mu sync.Mutex
	var seen []inferencetest.Seen
	var asked int
	var far *stub
	return inferencetest.Leg{
		Name: "the openai adapter over a stub endpoint",
		Build: func(t *testing.T, end inferencetest.End, cfg inference.Config) inference.Engine {
			mu.Lock()
			seen, asked = nil, 0
			mu.Unlock()
			far = &stub{t: t, end: end, mu: &mu, seen: &seen, asked: &asked, total: &openaiTotal}
			server := httptest.NewServer(far)
			t.Cleanup(server.Close)
			provider, err := openai.New(cfg, openai.Config{BaseURL: server.URL, APIKey: "test-key", Insecure: true})
			if err != nil {
				t.Fatalf("openai.New: %v", err)
			}
			engine, err := inference.New(cfg, provider)
			if err != nil {
				t.Fatalf("inference.New: %v", err)
			}
			t.Cleanup(func() { engine.Close() })
			return engine
		},
		Counter: func(t *testing.T) inference.Counter { return limit.Memory() },
		Sent: func(t *testing.T) []inferencetest.Seen {
			mu.Lock()
			defer mu.Unlock()
			return append([]inferencetest.Seen(nil), seen...)
		},
		Asked:       func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return asked },
		Uncountable: func(t *testing.T) inference.Counter { return unreachable{} },
		Total:       func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return openaiTotal },
	}
}

// stub is an endpoint that answers what a case says, in the shape of a vendor: json
// out, and the ask recovered from the body it was posted.
type stub struct {
	t     *testing.T
	end   inferencetest.End
	mu    *sync.Mutex
	seen  *[]inferencetest.Seen
	asked *int
	total *int
	n     int
}

func (s *stub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	s.record(body)
	if s.end.Silence > 0 {
		select {
		case <-time.After(s.end.Silence):
		case <-r.Context().Done():
			return
		}
	}
	n := s.count()
	if len(s.end.Answers) == 0 {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[]}`)
		return
	}
	answer := s.end.Answers[min(n, len(s.end.Answers)-1)]
	out := map[string]any{"model": answer.Model, "choices": []map[string]any{{
		"message":       map[string]any{"role": "assistant", "content": answer.Text},
		"finish_reason": orDefault(answer.Finish, "stop"),
	}}}
	if answer.Usage.Reported {
		out["usage"] = map[string]any{"prompt_tokens": answer.Usage.InputTokens, "completion_tokens": answer.Usage.OutputTokens}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		panic(err)
	}
}

func (s *stub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.n
	s.n++
	return n
}

// record recovers what the adapter sent, from the json body rather than the Go value:
// the picture is decoded out of the data URL, which is what the endpoint sees and the
// only reason this suite can call itself one.
func (s *stub) record(body []byte) {
	var posted struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &posted); err != nil {
		panic(err)
	}
	one := inferencetest.Seen{}
	for _, message := range posted.Messages {
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			if message.Role == "system" {
				one.Instruction = text
			} else {
				one.Prompt, one.Parts = text, 1
			}
			continue
		}
		var parts []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Image struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		if err := json.Unmarshal(message.Content, &parts); err != nil {
			panic(err)
		}
		one.Parts = len(parts)
		for _, part := range parts {
			switch {
			case part.Type == "text":
				one.Prompt = part.Text
			case strings.HasPrefix(part.Image.URL, "data:image/png;base64,"):
				one.Image = decodeBase64(s.t, strings.TrimPrefix(part.Image.URL, "data:image/png;base64,"))
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	*s.seen = append(*s.seen, one)
	*s.asked++
	*s.total++

}

// TestNoPictureReachesATraceAttributeEventOrError runs a marked ask through each leg
// with a real span recorder installed, and refuses any record that holds a word of it.
//
// The port promises this in its package comment; a promise about telemetry can only be
// checked by reading telemetry. The recorder is the SDK's own, imported by a test and
// by nothing in the package — which is what the boundary in scripts/check_packages.sh
// is for.
func TestNoPictureReachesATraceAttributeEventOrError(t *testing.T) {
	const (
		prompt      = "the drawing of my daughter's cat, Mr Whiskers"
		instruction = "name the animal, nothing else"
		answer      = "Mr Whiskers, who is a cat"
	)
	image := samplePNG(t, 4, 4)
	for _, leg := range []inferencetest.Legs{scriptLeg, openaiLeg} {
		t.Run(leg(t).Name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otelTracerProvider()
			setTracerProvider(provider)
			t.Cleanup(func() {
				setTracerProvider(previous)
				provider.Shutdown(context.Background())
			})

			engine, _ := open(t, leg, inferencetest.End{Answers: []inference.Response{{Text: answer, Model: "vendor-model",
				Usage: inference.Usage{InputTokens: 11, OutputTokens: 4, Reported: true}}}}, tenancy.WithTenant(context.Background(), tenant))
			if _, err := engine.Chat(tenancy.WithTenant(context.Background(), tenant), inference.Request{
				Instruction: instruction, Prompt: prompt, Image: image,
			}); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			for _, span := range recorder.Ended() {
				if len(span.Events()) != 0 {
					t.Errorf("the span carries %d event(s); an event message is a sentence a wrapper may rewrite "+
						"with the caller's text inside it", len(span.Events()))
				}
				if span.Status().Description != "" {
					t.Errorf("the span's status description is %q; only a class name belongs there", span.Status().Description)
				}
				for _, attr := range span.Attributes() {
					value := attr.Value.Emit()
					for _, marked := range []string{prompt, instruction, answer} {
						if strings.Contains(value, marked) {
							t.Errorf("%s = %q carries %q; a prompt is not an attribute", attr.Key, value, marked)
						}
					}
				}
			}
		})
	}
}

// TestTheSpanNamesTheModelAndNothingElse asks the same span the positive question: the
// keys it must carry, and the two counts it may only carry when the endpoint reported
// them.
func TestTheSpanNamesTheModelAndNothingElse(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otelTracerProvider()
	setTracerProvider(provider)
	t.Cleanup(func() {
		setTracerProvider(previous)
		provider.Shutdown(context.Background())
	})
	engine, _ := open(t, scriptLeg, inferencetest.End{Answers: []inference.Response{{Text: "a cat", Model: "span-test",
		Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}},
		tenancy.WithTenant(context.Background(), tenant))
	engine.Chat(tenancy.WithTenant(context.Background(), tenant), inference.Request{Prompt: "what is this?"})
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("%d span(s); one call opens exactly one", len(spans))
	}
	seen := map[string]bool{}
	for _, attr := range spans[0].Attributes() {
		seen[string(attr.Key)] = true
	}
	for _, want := range []string{"gen_ai.operation.name", "gen_ai.provider.name", "gen_ai.request.model",
		"gen_ai.response.model", telemetry.AttrTenantID} {
		if !seen[want] {
			t.Errorf("the span carries no %s; attributes: %v", want, spans[0].Attributes())
		}
	}
	if !seen["gen_ai.usage.input_tokens"] || !seen["gen_ai.usage.output_tokens"] {
		t.Error("reported token counts did not reach the span")
	}
}

// open is the suite's own builder, for the cases below that need a span recorder
// rather than a leg: same configuration, same counter, one engine.
func open(t *testing.T, fresh inferencetest.Legs, end inferencetest.End, with context.Context) (inference.Engine, context.Context) {
	t.Helper()
	leg := fresh(t)
	engine := leg.Build(t, end, inference.Config{
		Model: "span-test", DailyCalls: 10, Window: time.Hour, MaxAnswerBytes: 128, Timeout: 5 * time.Second,
		Counter: leg.Counter(t),
	})
	return engine, with
}

// unreachable is the counter of an installation whose store is not there: no pool,
// no process, a grant refused.
type unreachable struct{}

func (unreachable) Count(context.Context, string, time.Duration) (int, time.Duration, error) {
	return 0, 0, errors.New("inference test: the counter is not answering")
}

func (unreachable) Allow(context.Context, string, int, time.Duration) (bool, time.Duration, error) {
	return false, 0, errors.New("inference test: the counter is not answering")
}

func decodeBase64(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("the endpoint received %q, which is not base64: %v", s, err)
	}
	return raw
}

func otelTracerProvider() oteltrace.TracerProvider { return otel.GetTracerProvider() }
func setTracerProvider(p oteltrace.TracerProvider) { otel.SetTracerProvider(p) }

// samplePNG is a real PNG, encoded rather than hand-written: a case that typed a
// header would test the signature and nothing else.
func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func orDefault(v, when string) string {
	if v == "" {
		return when
	}
	return v
}

func boolToInt(ok bool) int {
	if ok {
		return 1
	}
	return 0
}
