package httpx_test

// A caller's trace state reaches the handler bounded. This is a pin, not a defect
// report: nothing here fails at the head it was written against.
//
// MaxTraceState is applied, the commit says, "at the one door through which a
// caller's state can enter — so the row, the envelope and the header can only
// ever carry the same bounded value". The door is real, and it is this package:
// kit/httpx/request_id.go:53 is the only call in the repository that puts a
// caller's trace context into a context. But the bound is tested one package
// away from the door it guards — kit/trace calls trace.Parse, and the trace
// package's own case calls trace.Parse:
//
// 	$ grep -rln "trace.Parse" --include=*.go .
// 	kit/trace/callers_trace_state_arrives_bounded_test.go
// 	kit/trace/trace.go
// 	kit/httpx/request_id.go
//
// so nothing asks what a *request* gets. If the middleware stopped calling
// trace.Parse — a renamed header, a second middleware that reads `tracestate`
// itself, a handler that rebuilds the context from the header it wants — the
// unit tests in kit/trace would stay green while a megabyte of caller's header
// reached platformkit_outbox.tracestate again, which is the defect the bound
// exists to prevent. What this case pins is the seam, not the arithmetic: the
// value a request's own context carries is the bounded one, whatever the caller
// sent, and the trace it keeps is the one the caller sent.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/trace"
)

func TestACallersTracestateReachesTheHandlersContextWithinTheBound(t *testing.T) {
	const parent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	api, router, _ := setup(t)
	var (
		had    bool
		got    trace.Context
		routed string
	)
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "trace-probe", Method: http.MethodGet, Path: "/trace-probe",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		got, had = trace.From(ctx)
		routed = "here"
		return &body{}, nil
	})

	for _, tc := range []struct {
		name  string
		state string
		// wantState is what the handler's context has to carry. A state that
		// fits arrives verbatim; a state that does not arrives as whole
		// entries within the kernel's bound, which is what this case measures
		// rather than the way the bound is cut.
		wantState string
	}{
		{
			name:      "a caller's ordinary state arrives verbatim",
			state:     "ddt=qZfHpR4R3pF79i4YBiyyhq2t,scr=00f067aa0ba902b7",
			wantState: "ddt=qZfHpR4R3pF79i4YBiyyhq2t,scr=00f067aa0ba902b7",
		},
		{
			name:  "a caller's oversized state arrives bounded",
			state: "ddt=" + strings.Repeat("x", 600) + ",scr=" + strings.Repeat("y", 600),
		},
		{
			name:  "a caller's state at the ceiling of a header block arrives bounded",
			state: strings.Repeat("k", 1<<20),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			had, routed, got = false, "", trace.Context{}
			req := httptest.NewRequest(http.MethodGet, "http://"+host+at(api, "/trace-probe"), nil)
			req.Header.Set(trace.ParentHeader, parent)
			req.Header.Set(trace.StateHeader, tc.state)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if routed != "here" {
				t.Fatalf("the probe answered %q, not %q: the request never reached a handler to be measured (code %d)",
					routed, "here", w.Code)
			}
			if !had {
				t.Fatal("the middleware handed the handler no trace context for a request that carried a valid traceparent")
			}
			if len(got.TraceState) > trace.MaxTraceState {
				t.Errorf("a handler's context carries a tracestate of %d bytes, more than the kernel's own bound of %d: the bound is written in trace.Parse, and this is the request that goes through it",
					len(got.TraceState), trace.MaxTraceState)
			}
			if tc.wantState != "" && got.TraceState != tc.wantState {
				t.Errorf("handler's tracestate = %q, want the caller's %q kept verbatim when it fits",
					got.TraceState, tc.wantState)
			}
			// The trace itself is not the price of the bound.
			if got.Parent() != parent {
				t.Errorf("handler's Parent() = %q, want the caller's %q", got.Parent(), parent)
			}
		})
	}
}
