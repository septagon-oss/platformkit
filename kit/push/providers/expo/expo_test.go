package expo_test

import (
	"context"
	"encoding/json"
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

	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/expo"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

const (
	expoToken = "ExponentPushToken[xxxxxxxxxxxxxxxxxxxxxx]"
	// webSubscription is a token of another platform: a well-formed browser
	// subscription, which the Expo adapter has no business carrying.
	webSubscription = `{"endpoint":"https://push.example/1","keys":{"auth":"aaaaaaaaaaa","p256dh":"bbbbbb"}}`
)

func TestThePushServiceAnswersForTheDevice(t *testing.T) {
	pushtest.Run(t, harness())
}

// harness opens one stand-in push service per case: what the adapter is tested
// against is the HTTP answer Expo's reference documents, ticket and all, and
// never the service itself.
func harness() pushtest.Harness {
	tokens := pushtest.Tokens{
		Carryable:       expoToken,
		Uncarryable:     "This page is no longer available.",
		ForeignPlatform: webSubscription,
	}
	return pushtest.Harness{
		Port:   "kit/push/providers/expo",
		Answer: push.Accepted,
		Tokens: tokens,
		States: []pushtest.State{pushtest.Healthy, pushtest.Gone, pushtest.Unavailable, pushtest.Refused},
		World: func(t *testing.T, run func(pushtest.Fixture)) {
			o := observe(t)
			service := newService(t)
			adapters := expo.New(expo.Config{
				BaseURL:    service.server.URL,
				HTTPClient: service.server.Client(),
			})
			run(pushtest.Fixture{
				Ctx:      t.Context(),
				Adapter:  adapters.Get(push.PlatformExpo),
				Platform: push.PlatformExpo,
				Tokens:   tokens,
				Device:   device,
				Router:   func() push.Adapters { return adapters },
				Stand:    service,
				Put:      service.put,
				Requests: service.getRequests,
				LastBody: service.lastBody,
				Spans:    o.spans,
				Lines:    o.lines,
			})
		},
		Extra: []porttest.Case[pushtest.Fixture]{theWireCarriesTheDeepLinkTheCollapseKeyAndAClock},
	}
}

func device(token string) push.Device {
	return push.Device{
		ID:       uuid.MustParse("3a3a3a3a-3a3a-3a3a-3a3a-3a3a3a3a3a3a"),
		TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Platform: push.PlatformExpo,
		Token:    token,
	}
}

// theWireCarriesTheDeepLinkTheCollapseKeyAndAClock names what the phone needs
// from this adapter and no case of the shared suite reads: the link it opens, the
// key it collapses on, and a TTL that is neither zero nor Expo's own four weeks.
var theWireCarriesTheDeepLinkTheCollapseKeyAndAClock = porttest.Case[pushtest.Fixture]{
	Name: "Send: the wire carries the deep link, the collapse key and a TTL of the port's own",
	Because: "the harness describes that a send reached the provider, not the fields it " +
		"is made of, and each of these three is a phone's behaviour rather than this package's",
	Run: func(t *testing.T, f pushtest.Fixture) {
		f.Put(t, pushtest.Healthy)
		m := pushtest.Message()
		if _, err := f.Adapter.Send(f.Ctx, device(f.Tokens.Carryable), m); err != nil {
			t.Fatalf("send: %v", err)
		}
		var sent map[string]any
		if err := json.Unmarshal([]byte(serviceOf(t, f).lastBody()), &sent); err != nil {
			t.Fatalf("the adapter posted %q, which is not JSON: %v", serviceOf(t, f).lastBody(), err)
		}
		if sent["to"] != f.Tokens.Carryable {
			t.Errorf("the message went to %v, want the device's token", sent["to"])
		}
		if ttl, ok := sent["ttl"].(float64); !ok || ttl < 1 {
			t.Errorf("the message carries ttl %v, and Expo drops a normal-priority message at zero on a dozing device", sent["ttl"])
		}
		if sent["collapseId"] != m.Tag {
			t.Errorf("the message collapses on %v, want the tag %q", sent["collapseId"], m.Tag)
		}
		data, _ := sent["data"].(map[string]any)
		if data["link"] != m.URL {
			t.Errorf("the message carries link %v, want %q", data["link"], m.URL)
		}
	},
}

// service is the stand-in push service: the answers Expo's reference documents,
// the requests it received, and the state the next answer comes from.
type service struct {
	server *httptest.Server

	mu       sync.Mutex
	state    pushtest.State
	requests int
	body     string
}

func newService(t *testing.T) *service {
	s := &service{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s.mu.Lock()
		defer s.mu.Unlock()
		s.requests++
		s.body = string(raw)
		switch s.state {
		case pushtest.Gone:
			write(w, http.StatusOK, `{"data":{"status":"error","message":"\"`+expoToken+
				`\" is not a registered push notification recipient","details":{"error":"DeviceNotRegistered"}}}`)
		case pushtest.Unavailable:
			write(w, http.StatusServiceUnavailable, `{"errors":[{"message":"the push service is restarting"}]}`)
		case pushtest.Refused:
			write(w, http.StatusOK, `{"data":{"status":"error","message":"the message is too long","details":{"error":"MessageTooBig"}}}`)
		default:
			write(w, http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func write(w http.ResponseWriter, status int, body string) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
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

func (s *service) lastBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body
}

// serviceOf is the stand-in a case of this package's own reaches through the
// world's Stand field.
func serviceOf(t *testing.T, f pushtest.Fixture) *service {
	t.Helper()
	s, ok := f.Stand.(*service)
	if !ok {
		t.Fatalf("the world's stand-in is a %T, not this package's push service", f.Stand)
	}
	return s
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
