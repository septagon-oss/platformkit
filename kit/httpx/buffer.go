package httpx

// buffer.go withholds the response until the request transaction has committed,
// so a client never reads a row the database then rolled back.

import (
	"bytes"
	"context"
	"net/http"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// buffer is a response that has not been sent yet.
//
// A 200 written before the commit is a lie whenever the commit can still fail,
// and it can: a DEFERRABLE INITIALLY DEFERRED constraint is checked at COMMIT,
// a serialization failure is raised there, and so is the settings re-read
// kit/db does. Holding the response costs one copy of the body and turns those
// into the 500 they are.
//
// A handler that calls Flush is streaming and knows what it is doing: from that
// call on this is a passthrough, and its response reaches the wire before the
// commit like any other streaming response. A huma StreamResponse that never
// calls Flush is not streaming in any sense the network can see: it is buffered
// whole, like every other response, and it is bounded like every other response.
//
// The bound is maxBuffer. Holding a response costs one copy of it, and a
// download that is larger than that is a response nobody should be copying:
// past the limit the buffer sends what it holds and becomes a passthrough, with
// the same honest consequence as Flush — the bytes are on the wire before the
// commit, and a commit that then fails is a log line rather than a 500.
type buffer struct {
	http.ResponseWriter
	// header is what the response headers were when the buffer was built, so
	// reset can put them back. A Location set by a handler whose transaction
	// then failed to commit must not survive into the 500 that replaces it.
	header http.Header
	// public is the surface that promises nobody standing at it is remembered.
	// It is decided here because this is the writer the promise has to be kept
	// at: the response is only held for as long as it fits, and a promise that
	// holds while the body is small and is forgotten when it is not is the
	// promise a cache in front of the surface would read.
	public  bool
	cookies []string
	status  int
	body    bytes.Buffer
	direct  bool
	// carried is the refusal a handler answered with, kept beside the held response it is
	// about to become.
	//
	// The reason it is kept here rather than read back out of the body is that two facts
	// about a refusal are deliberately absent from the JSON: problem.Problem's Key names
	// the copy this shell ships for the verdict, and its Diagnostic says the detail is an
	// operator's rather than the reader's. A program reading a code needs neither, which is
	// why they are `json:"-"`. The one reader that needs both is the page this held response
	// may still become, and by the time negotiation reaches the body they are gone — so the
	// verdict arrives at that decision with its own metadata on it rather than a sentence
	// reconstructed from a body that never carried it. See (*API).renegotiate.
	carried *problem.Problem
}

// carry records which refusal a handler answered with. The first one wins: it is the
// handler's own verdict, and anything the chain writes over a held response — a failed
// commit, a public route that minted a cookie — replaces the whole answer rather than
// adding a second verdict to it.
func (b *buffer) carry(refused *problem.Problem) {
	if b.carried == nil {
		b.carried = refused
	}
}

// refusal is the carried verdict, or nil where no handler answered with one.
func (b *buffer) refusal() *problem.Problem { return b.carried }

// maxBuffer is the most a held response may hold. Two megabytes is far past any
// JSON document this API produces and far below anything worth copying.
const maxBuffer = 2 << 20

func (b *buffer) WriteHeader(status int) {
	b.withholdCookies()
	if b.direct {
		b.ResponseWriter.WriteHeader(status)
		return
	}
	if b.status == 0 {
		b.status = status
	}
}

func (b *buffer) Write(p []byte) (int, error) {
	// First thing, because a route that sets a cookie and then writes a body has
	// ordered them the way the surface's promise cannot survive: by the time the
	// first byte goes, so do the headers beside it.
	b.withholdCookies()
	if !b.direct && b.body.Len()+len(p) > maxBuffer {
		b.send()
	}
	if b.direct {
		return b.ResponseWriter.Write(p)
	}
	return b.body.Write(p)
}

func (b *buffer) Flush() {
	b.withholdCookies()
	b.send()
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (b *buffer) Unwrap() http.ResponseWriter { return b.ResponseWriter }

// send writes what is held, once.
func (b *buffer) send() {
	if b.direct {
		return
	}
	b.direct = true
	if b.status == 0 {
		b.status = http.StatusOK
	}
	// Before the status line, because this is the last moment the headers are
	// still ours to write.
	b.withholdCookies()
	b.ResponseWriter.WriteHeader(b.status)
	if b.body.Len() > 0 {
		_, _ = b.ResponseWriter.Write(b.body.Bytes())
	}
}

// withholdCookies is the public surface's promise kept at the writer rather than
// observed after the fact: every Set-Cookie the response carries is taken off it
// before anything reaches the wire, whether the handler minted it before the
// headers went or after they had gone — which is the case a streaming route, an
// export, a file, is written with, and the case the surface exists for.
//
// What it took is kept, so respond can name the route that broke the promise and
// answer with the 500 it deserves where a response is still there to replace.
// Withholding is not forgiveness: the bytes a streaming route already sent stay
// sent, which is why the log line is written even when the status cannot be.
func (b *buffer) withholdCookies() {
	if !b.public {
		return
	}
	set := b.Header().Values("Set-Cookie")
	if len(set) == 0 {
		return
	}
	b.cookies = append(b.cookies, set...)
	b.Header().Del("Set-Cookie")
}

// withheld is every cookie the surface's writer took off this response.
func (b *buffer) withheld() []string { return b.cookies }

// reset discards the held response, headers included, so another can replace
// it. It reports false once the response has begun, which is the case a caller
// cannot take back.
func (b *buffer) reset() bool {
	if b.direct {
		return false
	}
	b.status = 0
	b.body.Reset()
	h := b.ResponseWriter.Header()
	clear(h)
	for k, v := range b.header {
		h[k] = v
	}
	return true
}

// begun reports whether any of the response has reached the wire.
func (b *buffer) begun() bool { return b.direct }

func bufferFrom(ctx context.Context) (*buffer, bool) {
	b, ok := ctx.Value(bufferKey{}).(*buffer)
	return b, ok
}
