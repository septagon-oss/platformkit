package log_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/porttest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

// The log adapter is the composition's stand-in for every platform at once, so
// the suite runs against each one it claims to carry: an adapter wired under a
// platform key has to answer as that platform, including about who owns the
// device the send names.
func TestEveryPlatformThePortNamesIsCarriedToTheRecorder(t *testing.T) {
	for _, p := range push.Platforms {
		t.Run(string(p), func(t *testing.T) { pushtest.Run(t, harness(p)) })
	}
}

// A token with a control character in it is what a paste of a stack trace looks
// like; the recorder must refuse it the way a real adapter would, or a
// development machine accepts a row no production installation could carry.
func TestAControlCharacterIsNotAToken(t *testing.T) {
	if err := log.NewHub().Adapters().Get(push.PlatformExpo).Validate("a\rtoken"); err == nil {
		t.Error("the recorder accepted a token with a carriage return in it")
	}
}

func harness(p push.Platform) pushtest.Harness {
	tokens := pushtest.Tokens{
		Carryable:   "token-for-" + string(p),
		Uncarryable: "a\rtoken no client handed out",
	}
	return pushtest.Harness{
		Port:   "kit/push/providers/log for " + string(p),
		Answer: push.Delivered,
		Tokens: tokens,
		States: []pushtest.State{pushtest.Healthy},
		World: func(t *testing.T, run func(pushtest.Fixture)) {
			o := observe(t)
			hub := log.NewHub()
			hub.Logger = o.logg
			adapters := hub.Adapters()
			run(pushtest.Fixture{
				Ctx:      t.Context(),
				Adapter:  adapters.Get(p),
				Platform: p,
				Tokens:   tokens,
				Device: func(token string) push.Device {
					return push.Device{
						ID: uuid.MustParse("5a5a5a5a-5a5a-5a5a-5a5a-5a5a5a5a5a5a"), TenantID: tenant, UserID: recipient,
						Platform: p, Token: token,
					}
				},
				Router:   func() push.Adapters { return adapters },
				Put:      func(*testing.T, pushtest.State) {},
				Requests: func() int { return len(hub.Sent()) },
				LastBody: func() string { return "" },
				Spans:    o.spans,
				Lines:    o.lines,
			})
		},
		Extra: []porttest.Case[pushtest.Fixture]{recorderHoldsEachSendOnce},
	}
}

var (
	tenant    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	recipient = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

// recorderHoldsEachSendOnce checks what the recorder is for: a module test reads
// it to ask which notices reached the transport, and one whose order or count is
// wrong answers that question wrongly rather than visibly.
var recorderHoldsEachSendOnce = porttest.Case[pushtest.Fixture]{
	Name: "Send: the recorder holds each send once, in the order they arrived",
	Because: "the recorder is the assertion a module test reads, and no field of the " +
		"harness's Op describes what a stand-in keeps.",
	Run: func(t *testing.T, f pushtest.Fixture) {
		hub := log.NewHub()
		hub.Logger = discard()
		adapters := hub.Adapters()
		for _, tag := range []string{"first", "second", "third"} {
			m := pushtest.Message()
			m.Tag = tag
			if _, err := adapters.Get(f.Platform).Send(f.Ctx, f.Device(f.Tokens.Carryable), m); err != nil {
				t.Fatalf("send %s: %v", tag, err)
			}
		}
		sent := hub.Sent()
		if len(sent) != 3 {
			t.Fatalf("the recorder holds %d sends, want three", len(sent))
		}
		for i, want := range []string{"first", "second", "third"} {
			if sent[i].Message.Tag != want {
				t.Errorf("send %d is tagged %q, want %q", i, sent[i].Message.Tag, want)
			}
			if sent[i].Device.Platform != f.Platform {
				t.Errorf("send %d reached a device of %s, want %s", i, sent[i].Device.Platform, f.Platform)
			}
		}
	},
}

// observer is what one case's sends left behind: the spans on the trace and the
// lines in the log. Each adapter's test carries its own because the SDK that
// records a span is imported by tests and by kit/app alone — see the
// measurement boundary scripts/check_packages.sh holds.
type observer struct {
	rec  *tracetest.SpanRecorder
	buf  *syncBuffer
	logg *slog.Logger
}

func observe(t *testing.T) *observer {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("close the trace recorder: %v", err)
		}
	})
	buf := &syncBuffer{}
	return &observer{rec: rec, buf: buf, logg: slog.New(slog.NewTextHandler(buf, nil))}
}

func (o *observer) spans() []pushtest.Span {
	var out []pushtest.Span
	for _, s := range o.rec.Ended() {
		attrs := map[string]string{}
		for _, a := range s.Attributes() {
			attrs[string(a.Key)] = a.Value.Emit()
		}
		out = append(out, pushtest.Span{Name: s.Name(), Attrs: attrs})
	}
	return out
}

func (o *observer) lines() []string {
	text := o.buf.String()
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// syncBuffer is a bytes.Buffer a handler may write while the case reads it back.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
