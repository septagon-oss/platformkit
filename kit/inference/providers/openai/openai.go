// Package openai is the adapter this kit speaks a model's wire with.
//
// It is the OpenAI-compatible chat endpoint, which is the one shape a local
// Ollama, a vLLM, a LiteLLM proxy and a hosted vendor all answer: Ollama's own
// documentation says to "use the same OpenAI format and change the hostname".
// Nothing vendor-specific lives here — no `detail`, no `temperature`, no tools,
// no streaming, no second attempt — because the port above promises one ask, one
// answer, one spend, and every vendor field added is a field the other endpoints
// have to explain. Portability is not claimed by this comment; the one suite in
// kit/inference/inferencetest is what claims it, per adapter, per run.
package openai

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/septagon-oss/platformkit/kit/inference"
)

// Config is the endpoint an operator named. It is never read from a request: the
// credential is the installation's, not a person's.
type Config struct {
	// BaseURL is scheme://host[:port][/prefix], the endpoint the kit posts
	// v1/chat/completions to. No userinfo, no query, no fragment, and no path
	// ending in v1 or chat/completions: that doubled prefix is the common
	// misconfiguration, and it answers 404 for the rest of the deployment's life,
	// so it is refused by name at boot rather than discovered on the first ask.
	BaseURL string

	// APIKey is sent as a bearer token when it is not empty. It is never in an
	// error, a log line or a span — including a 401 whose body quotes it back.
	APIKey string

	// Insecure permits plain http, and only on loopback: a key over a network
	// somebody else can read is not a configuration, it is a leak with a schedule.
	Insecure bool
}

// ErrEndpoint is the refusal of this adapter's own configuration: an address that
// cannot be posted to, or cannot be posted to safely.
var ErrEndpoint = errors.New("openai: this endpoint is not one this adapter can post to")

// New validates the endpoint and returns the provider. It is not readiness: no
// dial happens here, and an installation whose endpoint is down boots and answers
// on the first ask, as every other adapter in this repository does.
func New(shared inference.Config, cfg Config) (inference.Provider, error) {
	shared, err := shared.Defaults()
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(cfg.BaseURL)
	if err != nil || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery ||
		base.Opaque != "" || strings.Contains(cfg.BaseURL, "#") {
		return nil, ErrEndpoint
	}
	// shared.Timeout is the one wall this adapter's own client is built to: the
	// engine has already put that deadline on the context, so a transport that
	// answered later than the caller gave up would be a connection nobody reads.
	if shared.Timeout <= 0 {
		return nil, ErrEndpoint
	}
	trimmed := strings.TrimRight(base.Path, "/")
	if strings.HasSuffix(trimmed, "v1") || strings.HasSuffix(trimmed, "chat/completions") {
		return nil, fmt.Errorf("%w: %s already names the path this adapter posts to", ErrEndpoint, base.Path)
	}
	// A key with a space or a control character in it is a truncated file, a
	// pasted header or a shell variable that never expanded. It is refused because
	// it is never a key, not because it is unsafe to send.
	if strings.ContainsFunc(cfg.APIKey, func(r rune) bool { return r < 33 || r > 126 }) {
		return nil, ErrEndpoint
	}
	loopback := base.Hostname() == "localhost" || net.ParseIP(base.Hostname()).IsLoopback()
	if cfg.Insecure {
		if base.Scheme != "http" || !loopback {
			return nil, fmt.Errorf("%w: plain http is only answered on loopback", ErrEndpoint)
		}
	} else if base.Scheme != "https" {
		return nil, fmt.Errorf("%w: %q is not https and Insecure was not set", ErrEndpoint, base.Scheme)
	}
	if cfg.APIKey == "" && !loopback {
		return nil, fmt.Errorf("%w: a key is required off loopback", ErrEndpoint)
	}
	// The transport is this adapter's own, with no proxy configuration read from
	// the environment: a deployment cannot acquire a corporate HTTP_PROXY as a
	// second, undeclared destination for the prompt and the bearer token.
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   shared.Timeout,
		ResponseHeaderTimeout: shared.Timeout,
		IdleConnTimeout:       30 * time.Second,
		ForceAttemptHTTP2:     true,
	}
	return &adapter{
		model:  shared.Model,
		target: base.JoinPath("v1", "chat/completions").String(),
		host:   base.Host,
		key:    cfg.APIKey,
		bound:  shared.MaxResponseBytes,
		client: &http.Client{
			Transport: transport,
			Timeout:   shared.Timeout,
			// A 302 to a host the operator never named would take the bearer token
			// with it, so a redirect is a refusal and never a hop.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		transport: transport,
	}, nil
}

type adapter struct {
	model     string
	target    string
	host      string
	key       string
	bound     int
	client    *http.Client
	transport *http.Transport
	closed    atomic.Bool
}

// Chat posts one ask and reads one answer. One attempt, always: this call is not
// idempotent, it costs the tenant, and a retry is the caller's decision to make
// with its own deduplication.
func (a *adapter) Chat(ctx context.Context, ask inference.Request) (inference.Response, error) {
	if a.closed.Load() {
		return inference.Response{}, inference.ErrClosed
	}
	if ctx.Err() != nil {
		return inference.Response{}, ctx.Err()
	}
	body, err := json.Marshal(a.envelope(ask))
	if err != nil {
		return inference.Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.target, bytes.NewReader(body))
	if err != nil {
		return inference.Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.key != "" {
		req.Header.Set("Authorization", "Bearer "+a.key)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			// The caller gave up; that is not the endpoint's failure, and calling
			// it one would put a cancellation in the provider-unreachable class
			// every operator reads as an outage.
			return inference.Response{}, ctxErr
		}
		return inference.Response{}, fmt.Errorf("%w: %s did not answer", inference.ErrProviderUnavailable, a.host)
	}
	defer resp.Body.Close()
	return a.read(resp)
}

// Close drops the idle connections. A second Close, and every Chat after it,
// answers ErrClosed.
func (a *adapter) Close() error {
	if !a.closed.CompareAndSwap(false, true) {
		return inference.ErrClosed
	}
	a.transport.CloseIdleConnections()
	return nil
}

// envelope is the whole of the request body, and it names no vendor field. No
// max_tokens: the answer bound is this kit's, and turning bytes into tokens would
// be inventing a tokenizer. No stream: the port has none. No detail: it is a
// vendor field and its presence changes what a vendor charges.
func (a *adapter) envelope(ask inference.Request) wireRequest {
	user := wireContent{Text: ask.Prompt}
	if len(ask.Image) > 0 {
		user.Parts = []wirePart{
			{Type: "text", Text: ask.Prompt},
			{Type: "image_url", Image: wireImage{URL: "data:image/png;base64," + base64.StdEncoding.EncodeToString(ask.Image)}},
		}
	}
	messages := make([]wireMessage, 0, 2)
	if ask.Instruction != "" {
		messages = append(messages, wireMessage{Role: "system", Content: wireContent{Text: ask.Instruction}})
	}
	messages = append(messages, wireMessage{Role: "user", Content: user})
	return wireRequest{Model: a.model, Messages: messages, Stream: false}
}

// read answers for one HTTP response. No body is ever copied into an error: a
// vendor's error page can echo the prompt it was sent, which is how a person's
// drawing reaches a log line.
func (a *adapter) read(resp *http.Response) (inference.Response, error) {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if resp.StatusCode >= 500 {
			return inference.Response{}, fmt.Errorf("%w: %s answered %s", inference.ErrProviderUnavailable, a.host, status(resp))
		}
		return inference.Response{}, fmt.Errorf("%w: %s answered %s", inference.ErrProviderRefused, a.host, status(resp))
	}
	if !mediaType(resp) {
		// A 200 that answers HTML is a proxy's page, a login wall or a captive
		// portal: the port does not read it, and parsing it as an answer is how a
		// sign-in form becomes a translation.
		return inference.Response{}, fmt.Errorf("%w: %s answered %s, not json", inference.ErrAnswerShape, a.host, status(resp))
	}
	if resp.ContentLength > int64(a.bound) {
		return inference.Response{}, fmt.Errorf("%w: the endpoint offered %d bytes against a limit of %d",
			inference.ErrResponseTooLarge, resp.ContentLength, a.bound)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(a.bound)+1))
	if err != nil {
		if ctxErr := resp.Request.Context().Err(); ctxErr != nil {
			return inference.Response{}, ctxErr
		}
		return inference.Response{}, fmt.Errorf("%w: reading %s failed", inference.ErrProviderUnavailable, a.host)
	}
	if len(raw) > a.bound {
		return inference.Response{}, fmt.Errorf("%w: the endpoint sent more than the %d bytes this installation reads",
			inference.ErrResponseTooLarge, a.bound)
	}
	var answer wireResponse
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&answer); err != nil {
		return inference.Response{}, fmt.Errorf("%w: %s answered a body this port does not read", inference.ErrAnswerShape, a.host)
	}
	if dec.Decode(new(any)) != io.EOF {
		return inference.Response{}, inference.ErrAnswerShape
	}
	return answer.response()
}

// status is the numeric code alone. The reason phrase a vendor sends is its own
// sentence about the ask, and this adapter quotes none of it.
func status(resp *http.Response) string { return strconv.Itoa(resp.StatusCode) }

// mediaType is the content type this adapter will parse, with its parameters
// gone. A proxy that answers HTML with a 200 is the case this refuses.
func mediaType(resp *http.Response) bool {
	media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return err == nil && (media == "application/json" || strings.HasSuffix(media, "+json"))
}

// The two shapes of this wire: what is posted, and what is read back. A field
// either port names that the other does not is a field nothing reads, so
// content is read through json.RawMessage rather than a string: a server that
// answers a list of parts does not answer this port, and silently joining parts
// would read a refusal as an answer.
type wireRequest struct {
	Messages []wireMessage `json:"messages"`
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
}

type wireMessage struct {
	Content any    `json:"content"`
	Role    string `json:"role"`
}

type wireContent struct {
	Text  string
	Parts []wirePart
}

func (c wireContent) MarshalJSON() ([]byte, error) {
	if c.Parts == nil {
		return json.Marshal(c.Text)
	}
	return json.Marshal(c.Parts)
}

type wirePart struct {
	Image wireImage `json:"image_url"`
	Text  string    `json:"text"`
	Type  string    `json:"type"`
}

type wireImage struct {
	URL string `json:"url"`
}

type wireResponse struct {
	Choices []struct {
		FinishReason string          `json:"finish_reason"`
		Message      json.RawMessage `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
	} `json:"usage"`
}

// response is the reading of one answer: the content must be a JSON string, and
// usage is reported only when the endpoint reported it.
func (w wireResponse) response() (inference.Response, error) {
	if len(w.Choices) == 0 {
		return inference.Response{}, inference.ErrAnswerEmpty
	}
	var message struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(w.Choices[0].Message, &message); err != nil || len(message.Content) == 0 {
		return inference.Response{}, inference.ErrAnswerShape
	}
	var text string
	if err := json.Unmarshal(message.Content, &text); err != nil {
		return inference.Response{}, inference.ErrAnswerShape
	}
	out := inference.Response{Text: text, Model: w.Model, Finish: w.Choices[0].FinishReason}
	out.Truncated = out.Finish == finishLength
	if w.Usage != nil && w.Usage.PromptTokens != nil && w.Usage.CompletionTokens != nil {
		out.Usage = inference.Usage{InputTokens: *w.Usage.PromptTokens, OutputTokens: *w.Usage.CompletionTokens, Reported: true}
	}
	return out, nil
}

// finishLength is the one finish_reason that means "there was more": the rest of
// them ("stop", "content_filter", and whatever a vendor invents next") are read by
// the port above, which returns an answer it cannot prove is whole to nobody.
const finishLength = "length"
