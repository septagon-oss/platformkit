package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// unflushable is the response writer a deployment gets when something in front of
// this process buffers whole answers: a plain writer, with no flush of any kind on
// it. It is what a load balancer that ignores `X-Accel-Buffering` looks like from
// in here.
type unflushable struct {
	status int
	body   int
	header http.Header
}

func (u *unflushable) Header() http.Header {
	if u.header == nil {
		u.header = http.Header{}
	}
	return u.header
}

func (u *unflushable) WriteHeader(status int) {
	if u.status == 0 {
		u.status = status
	}
}

func (u *unflushable) Write(p []byte) (int, error) {
	u.body += len(p)
	return len(p), nil
}

// TestFlushOverAWriterThatCannotFlushReportsTheTruth is the reason both wrappers
// carry FlushError and not only the void Flush of http.Flusher:
// http.ResponseController consults FlushError before Flusher and before Unwrap, so
// a wrapper that answers only Flush resolves to itself and every failure under it
// comes back as success. A stream reads that answer and believes its frames arrived.
//
// It is checked over the chain the package actually builds — secured over buffer
// over the connection — because the wrappers are where the lie would live: each
// does its own work, asks the writer below, and reports what it heard.
func TestFlushOverAWriterThatCannotFlushReportsTheTruth(t *testing.T) {
	base := &unflushable{}
	chain := &secured{ResponseWriter: &buffer{ResponseWriter: base}}

	if err := http.NewResponseController(chain).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Errorf("flushing a chain over a writer with no flush: %v; want http.ErrNotSupported, so a stream learns its bytes stopped here", err)
	}
	// The bytes did reach the writer underneath: the held response was sent when it
	// was asked to flush, and what failed is the flush of the connection. A refusal
	// to stream is therefore about the connection and not about the write.
	if base.status != http.StatusOK {
		t.Errorf("the held response reached the base writer with status %d, want 200", base.status)
	}

	// And the same chain over a writer that can flush answers nil, so the failure
	// above is a fact about the writer and not this package's own pessimism.
	recorder := httptest.NewRecorder()
	if err := http.NewResponseController(&secured{ResponseWriter: &buffer{ResponseWriter: recorder}}).Flush(); err != nil {
		t.Errorf("flushing a chain over a writer that can flush: %v", err)
	}
}

// TestAResponseThatCannotFlushIsRefusedBeforeAnyHeader is the question a stream
// handler asks before it has committed to an answer: CanStream. It walks to the
// writer at the bottom of the chain, because that is the one the bytes would reach,
// and it writes nothing while asking — a refusal has to stay a normal answer, which
// is the only reason the caller can still send a problem document.
func TestAResponseThatCannotFlushIsRefusedBeforeAnyHeader(t *testing.T) {
	for _, test := range []struct {
		name string
		w    http.ResponseWriter
		want bool
	}{
		{"the connection itself, which flushes", httptest.NewRecorder(), true},
		{"this package's own chain over a writer that flushes",
			&secured{ResponseWriter: &buffer{ResponseWriter: httptest.NewRecorder()}}, true},
		{"a chain whose base buffers whole answers",
			&secured{ResponseWriter: &buffer{ResponseWriter: &unflushable{}}}, false},
		{"a bare writer with no flush on it", &unflushable{}, false},
		{"no writer at all", nil, false},
	} {
		if got := CanStream(test.w); got != test.want {
			t.Errorf("CanStream(%s) = %v, want %v", test.name, got, test.want)
		}
	}

	// Asking is not answering: the refusal is worth having only if the response is
	// still unwritten when the handler hears it.
	base := &unflushable{}
	if CanStream(&secured{ResponseWriter: &buffer{ResponseWriter: base}}) {
		t.Fatal("a chain over a writer that cannot flush reported that it could stream")
	}
	if base.status != 0 || base.body != 0 {
		t.Errorf("asking whether a response can be streamed wrote status %d and %d byte(s) to the base writer", base.status, base.body)
	}
}
