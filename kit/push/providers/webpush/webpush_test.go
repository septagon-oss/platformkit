package webpush_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

const expoToken = "ExponentPushToken[xxxxxxxxxxxxxxxxxxxxxx]"

func TestThePushServiceAnswersForTheSubscription(t *testing.T) {
	pushtest.Run(t, harness())
}

// harness opens one stand-in push service per case. The subscription it hands
// out points at that server, which is the whole trick this adapter allows: the
// URL a send posts to arrived inside the stored token, so the suite drives a
// real encrypted request over loopback with no seam in the library.
func harness() pushtest.Harness {
	return pushtest.Harness{
		Port:   "kit/push/providers/webpush",
		Answer: push.Delivered,
		States: []pushtest.State{pushtest.Healthy, pushtest.Gone, pushtest.Unavailable, pushtest.Refused},
		World: func(t *testing.T, run func(pushtest.Fixture)) {
			o := observe(t)
			service := newService(t)
			tokens := pushtest.Tokens{
				Carryable:   service.subscription(t),
				Uncarryable: "this is not a subscription",
				// Another platform's credential, well-formed there and
				// nonsense here: the case this token exists for.
				ForeignPlatform:   expoToken,
				PlainHTTPEndpoint: `{"endpoint":"http://push.example/1","keys":{"auth":"aaaaaaaaaaaa","p256dh":"` + service.p256dh + `"}}`,
			}
			public, private := vapidKeys(t)
			adapters := webpush.New(webpush.Config{
				PublicKey: public, PrivateKey: private,
				Subscriber: "mailto:push@example.test", HTTPClient: service.server.Client(),
			})
			run(pushtest.Fixture{
				Ctx:      t.Context(),
				Adapter:  adapters.Get(push.PlatformWeb),
				Platform: push.PlatformWeb,
				Tokens:   tokens,
				Device:   device,
				Router:   func() push.Adapters { return adapters },
				Stand:    service,
				Put:      service.put,
				Requests: service.getRequests,
				// The request body is encrypted for the subscription, so no
				// stand-in reads the words back; the port's own clip is
				// asserted on the span the send leaves behind.
				LastBody: func() string { return "" },
				Spans:    o.spans,
				Lines:    o.lines,
			})
		},
	}
}

// vapidKeys is a P-256 pair in the encoding the library reads: the raw scalar
// and the uncompressed point, base64url without padding. It is built here rather
// than with the library's own generator because the test must be able to say what
// a credential is even when the library that consumes it changes.
func vapidKeys(t *testing.T) (public, private string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("make a VAPID pair: %v", err)
	}
	secret := make([]byte, 32)
	key.D.FillBytes(secret)
	return base64.RawURLEncoding.EncodeToString(elliptic.Marshal(key.Curve, key.X, key.Y)),
		base64.RawURLEncoding.EncodeToString(secret)
}

func device(token string) push.Device {
	return push.Device{
		ID:       uuid.MustParse("4b4b4b4b-4b4b-4b4b-4b4b-4b4b4b4b4b4b"),
		TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Platform: push.PlatformWeb,
		Token:    token,
	}
}

// service is the stand-in push service: the subscription it will accept, the
// status it answers with, and how many requests arrived.
type service struct {
	server *httptest.Server
	p256dh string
	auth   string

	mu       sync.Mutex
	state    pushtest.State
	requests int
}

func newService(t *testing.T) *service {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("make a subscription key: %v", err)
	}
	s := &service{
		p256dh: base64.RawURLEncoding.EncodeToString(elliptic.Marshal(key.Curve, key.X, key.Y)),
		auth:   base64.RawURLEncoding.EncodeToString([]byte("0123456789abcdef")),
	}
	s.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests++
		switch s.state {
		case pushtest.Gone:
			w.WriteHeader(http.StatusGone)
		case pushtest.Unavailable:
			w.WriteHeader(http.StatusTooManyRequests)
		case pushtest.Refused:
			w.WriteHeader(http.StatusBadRequest)
		default:
			w.Header().Set("Location", "https://push.example/m/1")
			w.WriteHeader(http.StatusCreated)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *service) subscription(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(`{"endpoint":%q,"keys":{"auth":%q,"p256dh":%q}}`, s.server.URL+"/1", s.auth, s.p256dh)
}

func (s *service) put(t *testing.T, state pushtest.State) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

func (s *service) getRequests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
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

// syncBuffer is a buffer a handler may write while the case reads it back.
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
