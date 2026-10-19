package openai_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	png "image/png"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/providers/openai"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// ask is one text ask, whole, under every bound.
var ask = inference.Request{Instruction: "name the animal", Prompt: "what is this?"}

// answer is the body an endpoint that worked sends.
const answer = `{"model":"vendor-model","choices":[{"message":{"role":"assistant","content":"a cat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`

// TestAnEndpointThisAdapterCannotPostToIsRefusedAtBoot walks the refusals of the
// operator's own configuration. None of them makes a request: an address that cannot
// be posted to is a mistake in a file, and finding it on the first ask means a
// deployment boots broken and says so only when a person is waiting.
func TestAnEndpointThisAdapterCannotPostToIsRefusedAtBoot(t *testing.T) {
	const key = "sk-test-key"
	for _, tc := range []struct {
		name string
		cfg  openai.Config
	}{
		{"no url", openai.Config{APIKey: key}},
		{"no host", openai.Config{BaseURL: "https://", APIKey: key}},
		{"credentials in the address", openai.Config{BaseURL: "https://user:pw@api.example", APIKey: key}},
		{"a query", openai.Config{BaseURL: "https://api.example/?key=1", APIKey: key}},
		{"a forced query", openai.Config{BaseURL: "https://api.example/?", APIKey: key}},
		{"a fragment", openai.Config{BaseURL: "https://api.example/#frag", APIKey: key}},
		{"an opaque address", openai.Config{BaseURL: "mailto:api.example", APIKey: key}},
		{"the path already names v1", openai.Config{BaseURL: "https://api.example/v1", APIKey: key}},
		{"the path already names the call", openai.Config{BaseURL: "https://api.example/v1/chat/completions", APIKey: key}},
		{"plain http with no Insecure", openai.Config{BaseURL: "http://api.example", APIKey: key}},
		{"plain http, Insecure, a host off loopback", openai.Config{BaseURL: "http://api.example", APIKey: key, Insecure: true}},
		{"a key with a space in it", openai.Config{BaseURL: "https://api.example", APIKey: "sk 1"}},
		{"a key with a newline in it", openai.Config{BaseURL: "https://api.example", APIKey: "sk\n1"}},
		{"no key, off loopback", openai.Config{BaseURL: "https://api.example"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := openai.New(config(), tc.cfg)
			if !errors.Is(err, openai.ErrEndpoint) {
				t.Errorf("New(%s) = %v, want ErrEndpoint", tc.name, err)
			}
			if provider != nil {
				t.Errorf("New(%s) returned a provider beside its refusal", tc.name)
			}
		})
	}
	t.Run("a local endpoint with no key is the documented local case", func(t *testing.T) {
		// Ollama's own OpenAI-compatibility example is http://localhost:11434 with
		// no credential in it. Refusing that would refuse the deployment the brief
		// names, on the way to proving this adapter can speak to a vendor.
		if _, err := openai.New(config(), openai.Config{BaseURL: "http://localhost:11434", Insecure: true}); err != nil {
			t.Errorf("New(local, no key) = %v, want it accepted", err)
		}
	})
}

// TestATextOnlyAskIsSentAsOnePlainString reads the request bytes of the two shapes
// this adapter posts. Text-only is a plain string and not a one-part array, because
// the endpoints that reject an array where a string is expected are more numerous
// than the ones that reject the reverse — and the only way to know which one you
// sent is to look at the bytes.
func TestATextOnlyAskIsSentAsOnePlainStringAndAPictureAskAsTwoParts(t *testing.T) {
	var posted []byte
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posted, _ = io.ReadAll(r.Body)
		header = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, answer)
	}))
	defer server.Close()
	engine := dial(t, server)

	if _, err := engine.Chat(withTenant, ask); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	var text struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
			Role    string          `json:"role"`
		} `json:"messages"`
		Stream *bool  `json:"stream"`
		Model  string `json:"model"`
	}
	if err := json.Unmarshal(posted, &text); err != nil {
		t.Fatalf("the adapter posted %s, which is not json: %v", posted, err)
	}
	if len(text.Messages) != 2 || text.Messages[0].Role != "system" || text.Messages[1].Role != "user" {
		t.Fatalf("the ask arrived as %s; the steer travels in the system role and the question in the user's", posted)
	}
	if got := string(text.Messages[1].Content); got != `"what is this?"` {
		t.Errorf("a text-only ask arrived as %s, want the plain string", got)
	}
	if text.Stream == nil || *text.Stream {
		t.Error("the ask did not say stream:false; a port with no streaming does not ask an endpoint to start")
	}
	if text.Model != "test-model" {
		t.Errorf("model %q, want the configured one", text.Model)
	}
	if header.Get("Authorization") != "Bearer test-key" {
		t.Errorf("Authorization = %q, want the bearer token", header.Get("Authorization"))
	}
	if header.Get("Content-Type") != "application/json" || header.Get("Accept") != "application/json" {
		t.Errorf("the adapter posted headers %+v, want json both ways", header)
	}
	for _, field := range []string{"max_tokens", "temperature", "tools", "response_format", "detail", "n"} {
		if strings.Contains(string(posted), `"`+field+`"`) {
			t.Errorf("the ask carried %q; every vendor field is a figure this kit does not own and a price it "+
				"did not agree to", field)
		}
	}

	if _, err := engine.Chat(withTenant, inference.Request{Prompt: "what is this?", Image: picturePNG(t)}); err != nil {
		t.Fatalf("Chat(picture): %v", err)
	}
	var picture struct {
		Messages []struct {
			Content []struct {
				Type  string `json:"type"`
				Text  string `json:"text"`
				Image struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
			Role string `json:"role"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(posted, &picture); err != nil {
		t.Fatalf("the picture ask arrived as %s: %v", posted, err)
	}
	if len(picture.Messages) != 1 || picture.Messages[0].Role != "user" || len(picture.Messages[0].Content) != 2 {
		t.Fatalf("a picture ask arrived as %s; without a steer it is one message, of two parts", posted)
	}
	parts := picture.Messages[0].Content
	if parts[0].Type != "text" || parts[0].Text != "what is this?" {
		t.Errorf("the text part arrived as %+v", parts[0])
	}
	if !strings.HasPrefix(parts[1].Image.URL, "data:image/png;base64,") {
		t.Errorf("the picture arrived as %q, want a data URL with the png media type", parts[1].Image.URL)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(parts[1].Image.URL, "data:image/png;base64,"))
	if err != nil || string(raw) != string(picturePNG(t)) {
		t.Errorf("the picture decoded to %d bytes, not the %d it was sent with: %v", len(raw), len(picturePNG(t)), err)
	}
}

// TestARedirectCarriesNoKey asks the endpoint for a 302 to a second host. The
// request must be refused, and the second host must never hear of it: a bearer token
// that followed a redirect is a credential handed to whoever the operator's DNS or
// proxy pointed at.
func TestARedirectCarriesNoKey(t *testing.T) {
	followed := make(chan string, 1)
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		followed <- r.Header.Get("Authorization")
		io.WriteString(w, answer)
	}))
	defer elsewhere.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	}))
	defer server.Close()

	_, err := dial(t, server).Chat(withTenant, ask)
	if !errors.Is(err, inference.ErrProviderRefused) {
		t.Fatalf("Chat(302) = %v, want ErrProviderRefused", err)
	}
	select {
	case seen := <-followed:
		t.Fatalf("the redirect was followed, with Authorization %q", seen)
	case <-time.After(100 * time.Millisecond):
	}
}

// TestAnOverLargeResponseIsRefusedWithoutBeingRead takes the bound from both sides:
// the endpoint that says how big it is, and the one that says nothing and keeps
// talking. A byte bound enforced by reading the bytes is a memory limit, not a bound.
func TestAnOverLargeResponseIsRefusedWithoutBeingRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		length bool
	}{
		{"the endpoint names the length it is sending", true},
		{"the endpoint names nothing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := `{"model":"m","choices":[{"message":{"role":"assistant","content":"` +
					strings.Repeat("long ", 400) + `cat"},"finish_reason":"stop"}]}`
				w.Header().Set("Content-Type", "application/json")
				if tc.length {
					w.Header().Set("Content-Length", "90000")
					io.WriteString(w, strings.Repeat("x", 90000))
					return
				}
				w.Header().Set("Transfer-Encoding", "chunked")
				io.WriteString(w, body)
			}))
			defer server.Close()
			tight := inference.Config{DailyCalls: 10, Counter: limit.Memory(), Model: "test-model", Timeout: 2 * time.Second, MaxAnswerBytes: 8, MaxResponseBytes: 64}
			provider, err := openai.New(tight, openai.Config{BaseURL: server.URL, APIKey: "test-key", Insecure: true})
			if err != nil {
				t.Fatal(err)
			}
			engine, err := inference.New(tight, provider)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.Chat(withTenant, ask); !errors.Is(err, inference.ErrResponseTooLarge) {
				t.Errorf("Chat(over-large body) = %v, want ErrResponseTooLarge", err)
			}
		})
	}
}

// TestAContentThatIsNotAStringIsRefusedRatherThanFlattened runs the two bodies a
// vendor answers that this port does not read: a part list where a string belongs,
// and a body that is not json at all. Joining the parts would read a refusal as an
// answer, and neither is a shape an adapter may invent a meaning for.
func TestAContentThatIsNotAStringIsRefusedRatherThanFlattened(t *testing.T) {
	for _, tc := range []struct {
		name, body string
	}{
		{"a part list", `{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"a"}]},"finish_reason":"stop"}]}`},
		{"not json", `a cat`},
		{"an html page answering 200", `<html><body>a cat</body></html>`},
		{"no choices", `{"model":"m","choices":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", contentType(tc.body))
				io.WriteString(w, tc.body)
			}))
			defer server.Close()
			want := inference.ErrAnswerShape
			if tc.name == "no choices" {
				want = inference.ErrAnswerEmpty
			}
			if _, err := dial(t, server).Chat(withTenant, ask); !errors.Is(err, want) {
				t.Errorf("Chat(%s) = %v, want %v", tc.name, err, want)
			}
		})
	}
}

// TestNoKeyReachesAnErrorSentence takes the redaction case the endpoint can be made
// to write: a 401 whose body quotes the credential back. The error names the status
// and the host, and stops — because the sentence that reaches a log line is this
// adapter's, and the body it is quoting is somebody else's.
func TestNoKeyReachesAnErrorSentence(t *testing.T) {
	const key = "sk-live-abcdefghij"
	for _, status := range []int{http.StatusUnauthorized, http.StatusTeapot, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			io.WriteString(w, `{"error":{"message":"bad key `+key+`, prompt `+ask.Prompt+`","key":"`+key+`"}}`)
		}))
		defer server.Close()
		want := inference.ErrProviderRefused
		if status >= 500 {
			want = inference.ErrProviderUnavailable
		}
		_, err := dialWithKey(t, server, key).Chat(withTenant, ask)
		if !errors.Is(err, want) {
			t.Fatalf("Chat(%d) = %v, want %v", status, err, want)
		}
		for _, marked := range []string{key, ask.Prompt, ask.Instruction} {
			if strings.Contains(err.Error(), marked) {
				t.Errorf("the refusal %q quotes %q", err, marked)
			}
		}
	}
}

// withTenant is the context every ask in this file carries: the port refuses one
// that belongs to nobody, and an adapter's own cases are not about that refusal.
var withTenant = tenancy.WithTenant(context.Background(), tenancy.Tenant{
	ID: uuid.MustParse("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"), Slug: "asking",
})

// config is one model and one wall, the smallest thing New accepts.
func config() inference.Config {
	return inference.Config{DailyCalls: 10, Counter: limit.Memory(), Model: "test-model", Timeout: 2 * time.Second}
}

func dial(t *testing.T, server *httptest.Server) inference.Engine {
	t.Helper()
	return dialWithKey(t, server, "test-key")
}

func dialWithKey(t *testing.T, server *httptest.Server, key string) inference.Engine {
	t.Helper()
	engine, err := inference.New(config(), mustDialKey(t, server, key))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { engine.Close() })
	return engine
}

func mustDial(t *testing.T, server *httptest.Server) inference.Provider {
	t.Helper()
	return mustDialKey(t, server, "test-key")
}

func mustDialKey(t *testing.T, server *httptest.Server, key string) inference.Provider {
	t.Helper()
	provider, err := openai.New(config(), openai.Config{BaseURL: server.URL, APIKey: key, Insecure: true})
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func contentType(body string) string {
	if strings.HasPrefix(body, "{") {
		return "application/json"
	}
	return "text/html"
}

// png is a real PNG, four pixels of it, encoded rather than typed: a header written
// by hand tests the signature and nothing else.
func picturePNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := png.Encode(&out, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
