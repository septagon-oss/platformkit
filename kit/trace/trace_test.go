package trace_test

// The grammar is the whole of this package's contract, so it is pinned here
// rather than only where it is used. The envelope's own suite in kit/events
// proves a Context survives a round-trip through the outbox and the broker; it
// cannot show which header strings become a Context and which do not, and a
// format whose rejections nobody tests drifts towards "accept anything that
// looks roughly right" — which, for a value every event carries, is a wire
// format no bridge can rely on.
//
// The cases below come from the W3C Trace Context specification's own examples
// and from the two refusals this package adds meaning to: a version it cannot
// read is no context, and an identifier the specification reserves (all zero)
// is no context. Both refusals return false rather than an error, because a
// caller's broken header is not a reason to refuse their request, so the
// caller's next hop must still see a valid one.

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/trace"
)

const (
	// specParent is the specification's example parent: version 00, a 32-hex
	// trace id, a 16-hex span id and the sampled flag.
	specParent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
	specTrace  = "4bf92f3577b34da6a3ce929d0e0e4736"
	specSpan   = "00f067aa0ba902b7"

	traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	spanID  = "00f067aa0ba902b7"
)

func TestValidIsTheGrammarAndNotMerelyHex(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ctx     trace.Context
		want    bool
		because string
	}{
		{"the specification's example", trace.Context{TraceID: traceID, SpanID: spanID}, true, "32 and 16 lower-case hex"},
		{"upper-case hex", trace.Context{TraceID: strings.ToUpper(traceID), SpanID: spanID}, false, "W3C fixes lower-case, and an id that differs only in case is two ids"},
		{"a trace id one character short", trace.Context{TraceID: traceID[:31], SpanID: spanID}, false, "width is part of the format"},
		{"a trace id one character long", trace.Context{TraceID: traceID + "0", SpanID: spanID}, false, "width is part of the format"},
		{"a span id of the wrong width", trace.Context{TraceID: traceID, SpanID: spanID[:8]}, false, "width is part of the format"},
		{"a non-hex character", trace.Context{TraceID: traceID[:31] + "z", SpanID: spanID}, false, "hex, not any 32 characters"},
		{"the reserved all-zero trace id", trace.Context{TraceID: strings.Repeat("0", 32), SpanID: spanID}, false, "the specification reserves it, so an event must not claim it"},
		{"the reserved all-zero span id", trace.Context{TraceID: traceID, SpanID: strings.Repeat("0", 16)}, false, "the specification reserves it too"},
		{"no context at all", trace.Context{}, false, "the zero value means absent, not empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ctx.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v: %s", got, tc.want, tc.because)
			}
		})
	}
}

// TestAParentIsWrittenInTheFormatW3CFixes pins the exact string. It is written
// by one function and read by foreign bridges — the envelope attribute, the
// outbox column and the response header are all its output — so a changed
// separator, a changed version byte or a padded id is a wire-format change and
// has to fail something here rather than a subscriber's parser in production.
func TestAParentIsWrittenInTheFormatW3CFixes(t *testing.T) {
	c := trace.Context{TraceID: traceID, SpanID: spanID, TraceState: "congo=t61rcWkgMzE"}
	want := "00-" + traceID + "-" + spanID + "-01"
	if got := c.Parent(); got != want {
		t.Errorf("Parent() = %q, want %q", got, want)
	}
	// The flag byte records "a collector will want this trace", the only honest
	// answer before there is a sampler; and the version is the one this package
	// writes, whatever arrived.
	if !strings.HasPrefix(c.Parent(), "00-") || !strings.HasSuffix(c.Parent(), "-01") {
		t.Errorf("Parent() = %q, want version 00 and the sampled flag", c.Parent())
	}
	// tracestate is carried verbatim by the value and never enters traceparent.
	if strings.Contains(c.Parent(), "congo") {
		t.Error("Parent() leaked tracestate into traceparent")
	}

	// An invalid context writes no header. "" rather than a malformed one is
	// what lets the outbox column stay NULL and the envelope omit the attribute:
	// the absence of a trace is a fact, and a zero-id header would be a lie.
	for name, c := range map[string]trace.Context{
		"the zero value":      {},
		"all-zero trace id":   {TraceID: strings.Repeat("0", 32), SpanID: spanID},
		"only a trace id":     {TraceID: traceID},
		"upper-case ids":      {TraceID: strings.ToUpper(traceID), SpanID: strings.ToUpper(spanID)},
		"an unparseable span": {TraceID: traceID, SpanID: "zzf067aa0ba902b7"},
	} {
		if got := c.Parent(); got != "" {
			t.Errorf("%s: Parent() = %q, want no header at all", name, got)
		}
	}
}

// TestParseAcceptsOnlyWhatItCanUnderstand is the caller-facing half: httpx calls
// this with whatever arrived, so every refusal has to be a refusal that keeps
// the request working, and every acceptance has to be the value the caller
// actually sent.
func TestParseAcceptsOnlyWhatItCanUnderstand(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, state string
		want          bool
		traceID       string
		spanID        string
	}{
		{"the specification's example", specParent, "", true, specTrace, specSpan},
		{"with vendor state", specParent, "congo=t61rcWkgMzE,foo=bar", true, specTrace, specSpan},
		{"a version newer than this package", "01-" + traceID + "-" + spanID + "-01", "", false, "", ""},
		{"the reserved version ff", "ff-" + traceID + "-" + spanID + "-01", "", false, "", ""},
		{"a version this package invented", "00v2-" + traceID + "-" + spanID + "-01", "", false, "", ""},
		{"an all-zero trace id", "00-" + strings.Repeat("0", 32) + "-" + spanID + "-01", "", false, "", ""},
		{"an upper-case trace id", "00-" + strings.ToUpper(traceID) + "-" + spanID + "-01", "", false, "", ""},
		{"a short span id", "00-" + traceID + "-00f067aa-01", "", false, "", ""},
		{"three fields", "00-" + traceID + "-" + spanID, "", false, "", ""},
		{"an empty header", "", "", false, "", ""},
		{"a bare separator", "-", "", false, "", ""},
		{"a caller's opaque handle", "not-a-trace", "", false, "", ""},
		// A version 00 parent with a field appended is this version's own
		// extension point: the unknown tail is ignored, not refused, because the
		// specification says to read what you understand and skip the rest.
		{"a trailing field on a known version", specParent + "-extra", "", true, specTrace, specSpan},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := trace.Parse(tc.parent, tc.state)
			if ok != tc.want {
				t.Fatalf("Parse(%q) ok = %v, want %v", tc.parent, ok, tc.want)
			}
			if !tc.want {
				// A refusal returns no value, so a caller cannot deliver a
				// half-parsed trace and cannot tell a bad header from none.
				if got != (trace.Context{}) {
					t.Errorf("Parse(%q) refused but returned %+v, want the zero value", tc.parent, got)
				}
				return
			}
			if got.TraceID != tc.traceID || got.SpanID != tc.spanID {
				t.Errorf("Parse(%q) = %+v, want trace %q span %q", tc.parent, got, tc.traceID, tc.spanID)
			}
			if got.TraceState != tc.state {
				t.Errorf("tracestate = %q, want it kept verbatim as %q", got.TraceState, tc.state)
			}
			// What Parse accepted, Parent writes back out in the same shape, so
			// the attribute an event carries is the header the caller sent.
			if !got.Valid() || got.Parent() == "" {
				t.Errorf("an accepted context does not re-render: %+v", got)
			}
		})
	}
}

// TestFromRequestIDIsTheJoin is the reason the package has a producer at all:
// the request id a caller quoted, the log line's request_id and the event's
// traceparent are one identifier when this returns true, and two unrelated
// strings when it returns false.
func TestFromRequestIDIsTheJoin(t *testing.T) {
	const dashed = "3ee9ee74-4a1d-4b96-a2f0-4f70ca6d0f4b"
	const undashed = "3ee9ee744a1d4b96a2f04f70ca6d0f4b"

	for _, tc := range []struct {
		name, id, as string
		want         bool
	}{
		{"a uuid as httpx generates it", dashed, undashed, true},
		{"the same uuid without separators", undashed, undashed, true},
		{"a 32-hex handle that is not a uuid", traceID, traceID, true},
		{"a proxy's opaque handle", "from-the-proxy", "", false},
		{"an empty id", "", "", false},
		{"the reserved all-zero uuid", "00000000-0000-0000-0000-000000000000", "", false},
		// W3C fixes lower-case, and this refuses rather than folding: a caller
		// whose ids are upper-case gets no traceparent and keeps their own id in
		// the response header, which is a missing join rather than a wrong one.
		{"an upper-case uuid", strings.ToUpper(dashed), "", false},
		{"a uuid with a character missing", "3ee9ee74-4a1d-4b96-a2f0-4f70ca6d0f4", "", false},
		{"33 hex characters", undashed + "0", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := trace.FromRequestID(tc.id)
			if ok != tc.want {
				t.Fatalf("FromRequestID(%q) ok = %v, want %v", tc.id, ok, tc.want)
			}
			if !tc.want {
				if got != (trace.Context{}) {
					t.Errorf("FromRequestID(%q) refused but returned %+v", tc.id, got)
				}
				return
			}
			if got.TraceID != tc.as {
				t.Errorf("trace id = %q, want the request id itself as %q", got.TraceID, tc.as)
			}
			// The span is this process's own: a fresh one, so a trace that
			// started at the caller is not attributed to a span it never had.
			span := trace.Context{TraceID: got.TraceID, SpanID: got.SpanID}
			if !span.Valid() || got.SpanID == strings.Repeat("0", 16) {
				t.Errorf("span id = %q, want a fresh valid span of this process's own", got.SpanID)
			}
		})
	}
}

// TestANewContextIsItsOwnTrace covers the branch a background request takes when
// nothing named it: the ids have to be well-formed, distinct between calls, and
// never the reserved all-zero — an id collision would merge two unrelated traces
// into one line of audit, and an all-zero id is a header no collector reads.
func TestANewContextIsItsOwnTrace(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		c := trace.New()
		if !c.Valid() {
			t.Fatalf("New() produced an invalid context: %+v", c)
		}
		if len(c.TraceID) != 32 || len(c.SpanID) != 16 {
			t.Fatalf("New() widths: %+v", c)
		}
		if c.TraceID == strings.Repeat("0", 32) || c.SpanID == strings.Repeat("0", 16) {
			t.Fatalf("New() produced a reserved identifier: %+v", c)
		}
		if seen[c.TraceID] {
			t.Fatalf("New() repeated a trace id: %s", c.TraceID)
		}
		seen[c.TraceID] = true
	}
}

// TestAContextTravelsThroughAContext is the hand-off from the request to the
// outbox write. The invalid case matters as much as the valid one: With must
// not store a context that is not one, or the writer downstream would find a
// trace where the request had none and stamp it onto an event.
func TestAContextTravelsThroughAContext(t *testing.T) {
	if _, ok := trace.From(context.Background()); ok {
		t.Error("From(context.Background()) found a trace, want none: a job that relayed a row has no request behind it")
	}

	c := trace.Context{TraceID: traceID, SpanID: spanID, TraceState: "congo=t61rcWkgMzE"}
	got, ok := trace.From(trace.With(t.Context(), c))
	if !ok {
		t.Fatal("the context a request carried did not survive the hand-off")
	}
	if got.TraceID != c.TraceID || got.SpanID != c.SpanID || got.TraceState != c.TraceState {
		t.Errorf("From() = %+v, want the value With() stored %+v", got, c)
	}

	ctx := trace.With(t.Context(), trace.Context{})
	if found, ok := trace.From(ctx); ok {
		t.Errorf("an invalid context was stored: %+v", found)
	}
	// And the writer of that absence is Parent(): no header, so the outbox
	// column stays NULL and the envelope omits the attribute.
	if (trace.Context{}).Parent() != "" {
		t.Error("the zero context writes a traceparent")
	}
}

// TestOversizedVendorStateIsKeptWholeOrDropped is the shape half of the bound on
// a caller's tracestate. A length cap could be met by slicing the header mid
// entry, and that is the one cure W3C forbids: a vendor's entry is opaque to
// the receiver, so half of it is not a smaller value, it is a wrong one. The
// rule that passes here is therefore the prefix of whole entries, in the order
// the caller sent them, with the rest dropped — and the trace kept, because the
// parent is the part this program writes into an event and the state is the
// caller's own memory of it.
func TestOversizedVendorStateIsKeptWholeOrDropped(t *testing.T) {
	entries := make([]string, 8)
	for i := range entries {
		entries[i] = "abcdefgh"[i:i+1] + "=" + strings.Repeat("x", 100)
	}
	// How many whole entries fit is derived from the module's own bound rather
	// than written down, so the case tests the rule and not one arithmetic.
	fits, size := 0, 0
	for i, entry := range entries {
		width := len(entry)
		if i > 0 {
			width += 2 // the ", " that separates entries in the header
		}
		if size+width > trace.MaxTraceState {
			break
		}
		size, fits = size+width, i+1
	}
	if fits == 0 || fits == len(entries) {
		t.Fatalf("the fixture neither fits nor overflows: %d of %d entries in %d bytes", fits, len(entries), trace.MaxTraceState)
	}

	got, ok := trace.Parse(specParent, strings.Join(entries, ","))
	if !ok {
		t.Fatal("a valid traceparent was refused because of its tracestate")
	}
	if want := strings.Join(entries[:fits], ", "); got.TraceState != want {
		t.Errorf("tracestate = %q, want the first %d entries kept whole and in the caller's order: %q", got.TraceState, fits, want)
	}
	// The trace survives its hint being trimmed. Parent() re-renders from the
	// parsed fields, so its width is a constant whatever the caller sent.
	if got.TraceID != specTrace || got.SpanID != specSpan {
		t.Errorf("the trace did not survive the trimmed state: got %+v, want trace %q span %q", got, specTrace, specSpan)
	}
	if got.Parent() == "" || len(got.Parent()) != 55 {
		t.Errorf("Parent() = %q, want the 55 canonical bytes whatever the caller's state was", got.Parent())
	}

	// One entry too large on its own drops the state rather than cutting it.
	if got, ok := trace.Parse(specParent, strings.Repeat("z", trace.MaxTraceState+1)); !ok || got.TraceState != "" {
		t.Errorf("an entry too large to keep = %q (ok = %v), want the state dropped whole and the trace kept", got.TraceState, ok)
	}
	// Under the ceiling nothing is rewritten: not the value, not its spacing.
	if got, _ := trace.Parse(specParent, "a=1, b=2"); got.TraceState != "a=1, b=2" {
		t.Errorf("a state within the bound came back as %q, want the caller's bytes untouched", got.TraceState)
	}
}
