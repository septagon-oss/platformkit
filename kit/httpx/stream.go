package httpx

// stream.go is what this package owes a route whose response is written over time
// rather than returned: a way to ask whether the response *can* be streamed, a way
// to say that one is being written, and the error-carrying form of the flush both
// need.

import (
	"context"
	"net/http"
	"time"
)

// StreamedResponseExtension marks an operation whose response is written over
// time: a stream that will outlive the question that opened it.
//
// It is the response-side twin of StreamedBodyExtension, and it buys the same
// thing: the transaction the guards above opened is committed before the handler is
// entered. Resolving the tenant, recognising the caller and checking the
// authorization are queries, so by the time a handler is entered an authenticated
// request already holds a transaction — right for a route whose work is a query, and
// wrong for one whose work is waiting. A stream is the second shape in its purest
// form: it holds a connection for minutes, and the rows that decided whether to open
// it are read once, at the start.
//
// See StreamedResponse for the door, and bodies.go for the commit.
const StreamedResponseExtension = "x-platformkit-streams-response"

// StreamAddress is the workspace address the kernel's live door answers at: the
// App surface's own root with "stream". No module owns the connection every
// module's declared events arrive on, so the kernel mounts it and the kernel
// names it, and a page that draws a live region links this one value rather than
// spelling an address that could drift from the route that answers it.
const StreamAddress = "/api/v1/app/stream"

// StreamedResponse is what a route that streams its response puts in its
// Extensions, beside an authorization declaration. It closes nothing else: the
// surface, the guard and the logging are unchanged, and the response's own media
// type is the handler's to write.
func StreamedResponse() (string, any) { return StreamedResponseExtension, true }

// CanStream reports whether a response could actually be streamed: whether the writer at the
// bottom of this chain — the one the bytes would reach — can flush. It writes nothing and asks
// nothing of the response, so a handler can ask it before it has committed to an answer, and
// a refusal is still a normal answer rather than a half-written one.
//
// The question is asked of the base writer and not of the wrappers above it, because that is
// where the two answers differ: both writers this package installs implement Flush, do their
// own work and then ask the writer underneath, so a chain whose base cannot flush is a chain
// whose topmost Flush would be called and return nothing — and a stream served over it would
// believe its frames arrived. A response that cannot be flushed is a deployment fault, a proxy
// or a wrapper buffering whole answers in front of this process, and the answer to it is the
// 500 that names it, before a single byte of a lie goes out.
func CanStream(w http.ResponseWriter) bool {
	if w == nil {
		return false
	}
	base := w
	for {
		unwrapper, ok := base.(interface{ Unwrap() http.ResponseWriter })
		if !ok || unwrapper.Unwrap() == nil {
			break
		}
		base = unwrapper.Unwrap()
	}
	switch base.(type) {
	case interface{ FlushError() error }, http.Flusher:
		return true
	}
	return false
}

// flushError asks one writer to flush in the only form that can answer honestly.
//
// It is the same three steps http.ResponseController takes, and it exists so the two
// wrappers of this package can report the truth about the writer underneath rather
// than their own void Flush's silence: ResponseController consults FlushError before
// Flusher and before Unwrap, so a wrapper that only has Flush resolves to itself and
// every failure below it is reported as success.
func flushError(w http.ResponseWriter) error {
	switch f := w.(type) {
	case interface{ FlushError() error }:
		return f.FlushError()
	case http.Flusher:
		f.Flush()
		return nil
	}
	return http.ErrNotSupported
}

// SetRetryAfter names, on the held response, how long the caller is to sit out
// before asking again. It is for a refusal this package cannot count — a handler
// that keeps its own limit and still owes the caller a number — and it sets nothing
// else: the caller still returns the problem document, and respond's floor of a
// minute stands for every 429 that names no wait.
//
// A response already on the wire cannot be told to wait, and this says so rather
// than writing a header nobody will read.
func SetRetryAfter(ctx context.Context, wait time.Duration) {
	b, ok := bufferFrom(ctx)
	if !ok || b.begun() {
		return
	}
	b.Header().Set(RetryAfterHeader, secondsUp(wait, 1))
}
