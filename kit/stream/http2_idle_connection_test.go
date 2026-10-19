package stream

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/module"
)

func TestAnIdleHTTP2StreamOutlivesItsWriteDeadline(t *testing.T) {
	limits := DefaultLimits()
	f := newFixture(t, []module.Stream{{Event: "task.closed", Policy: module.StreamAnyPrincipal()}}, limits)
	f.hub.clock = systemClock{}
	finished := make(chan error, 1)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := f.request(0, 0, "task.closed")
		req.HTTP = r
		finished <- f.hub.Serve(r.Context(), w, req)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), limits.Heartbeat+5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ProtoMajor != 2 {
		t.Fatalf("stream = %s %s; want HTTP/2 200", res.Proto, res.Status)
	}
	scanner := bufio.NewScanner(res.Body)
	opened := false
	for scanner.Scan() {
		if scanner.Text() == "event: resync" {
			opened = true
		}
		if opened && scanner.Text() == "" {
			break
		}
	}
	if !opened {
		t.Fatalf("stream never sent its opening resync: %v", scanner.Err())
	}
	// No frame is being written while the first heartbeat is still in the future.
	// A per-write deadline must not reset an otherwise healthy idle HTTP/2 stream.
	for scanner.Scan() {
		if scanner.Text() == ": ping" {
			cancel()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("the cancelled stream did not release its handler")
			}
			return
		}
	}
	t.Fatalf("idle HTTP/2 stream ended before its first heartbeat: %v", scanner.Err())
}
