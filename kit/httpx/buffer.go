package httpx

// buffer.go withholds the response until the request transaction has committed,
// so a client never reads a row the database then rolled back.

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/septagon-oss/platformkit/kit/problem"
)

// buffer is a response that has not been sent yet.
//
// A 200 written before the commit is a lie whenever the commit can still fail, and it
// can: a DEFERRABLE INITIALLY DEFERRED constraint is checked at COMMIT, a serialization
// failure is raised there, and so is the settings re-read kit/db does. Holding the
// response costs one copy of the body and turns those into the 500 they are.
//
// A handler that calls Flush is streaming and knows what it is doing: from that call on
// this is a passthrough. A huma StreamResponse that never calls Flush is buffered whole,
// like every other response, and bounded like every other response. Past maxBuffer the
// buffer sends what it holds and becomes a passthrough, with the same honest consequence:
// the bytes are on the wire before the commit, and a commit that then fails is a log line
// rather than a 500.
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
	// counted is every reading a limit put on this answer, in the order the guards
	// consulted them — the same order `RateLimit-Policy` lists them in. It is held
	// beside the response because a keyed write answers a retry from its record, from
	// a different request's call stack, and the numbers the counters gave are the only
	// account of the ceilings that answer was decided under.
	counted       []measured
	status        int
	body          bytes.Buffer
	direct        bool
	mutation      bool
	streamRefused bool
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
		if b.status == status {
			// The response is already on the wire with this status, so declaring it
			// again is not a change. The caller that does it is the route that wrote its
			// own response and then told the framework what it chose — a download, a
			// stream — and net/http's complaint about the second call is a log line per
			// connection about a difference that did not exist.
			return
		}
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
	if b.mutation && b.body.Len()+len(p) > maxBuffer {
		b.streamRefused = true
		return 0, errors.New("httpx: mutation response must be buffered")
	}
	if !b.direct && b.body.Len()+len(p) > maxBuffer {
		b.send()
	}
	if b.direct {
		return b.ResponseWriter.Write(p)
	}
	return b.body.Write(p)
}

func (b *buffer) Flush() {
	if b.mutation {
		b.streamRefused = true
		return
	}
	b.withholdCookies()
	b.send()
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// FlushError is Flush with the truth attached: this writer does its own work — the
// withheld cookies taken off, the headers and the body sent — and then asks the
// writer underneath in the form that can fail.
//
// Flush keeps its void signature, because that is http.Flusher's and callers outside
// this package have always used it. But http.ResponseController consults FlushError
// before Flusher, and before Unwrap: without this method a flush over a connection
// that cannot flush reports success, and a stream is the one caller that would never
// learn that its frames stopped arriving.
func (b *buffer) FlushError() error {
	b.withholdCookies()
	b.send()
	return flushError(b.ResponseWriter)
}

func (b *buffer) Unwrap() http.ResponseWriter { return b.ResponseWriter }

// noteCounted keeps the readings a limit reported on this answer, in the order they
// were taken, each stamped with the instant it reached the answer — which is the
// instant the counter answered, because every door that spends a counter states it at
// the statement. The stamp is what a window is closed against: a handler that went on
// working after its refusal must not add its own seconds to the wait a retry is told to
// sit out. It keeps none of a response that is already on the wire: past that point
// there is no answer left to remember, and a keyed write is spent rather than
// replayable. See buffer.counted.
func noteCounted(b *buffer, got ...measured) {
	if b == nil || b.direct {
		return
	}
	at := time.Now().UTC()
	for _, m := range got {
		m.seen = at
		b.counted = append(b.counted, m)
	}
}

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
	// The readings go with the headers: they describe the answer being thrown away,
	// and a limit stated on an answer nobody was given is a number nothing consulted.
	b.counted = nil
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
