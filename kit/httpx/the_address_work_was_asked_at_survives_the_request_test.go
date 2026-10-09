package httpx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTheAddressWorkWasAskedAtOutlivesTheRequestThatAsked is the half of
// ServedAuthority's rule that belongs to the worker rather than the request.
//
// Every mailed link is built after the request that caused it is gone, so the
// address has to be restored onto the handler's context — and a value restored from
// an outbox row must never be what a live request answers with, because the port a
// link carries is only ever a port this process accepted a connection on. Both
// halves are asked here, and the second one is asked of a live listener for exactly
// that reason: see TestTheServedAuthorityNamesAPortOnlyWhenTheCallNamedOne.
func TestTheAddressWorkWasAskedAtOutlivesTheRequestThatAsked(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// requestKey is what httpx.RequestFrom reads, and the middleware in
		// request.go is what normally puts it there. This is that line.
		ctx := context.WithValue(r.Context(), requestKey{}, r)
		served := ServedFrom(ctx)
		// Both answers travel in the body: a handler that wrote into a variable the
		// test read afterwards would be a data race wearing a test.
		fmt.Fprint(w, served+"|"+ServedFrom(WithServed(ctx, "elsewhere.localhost:9000")))
	}))
	defer server.Close()
	res, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	asked := string(body)
	host, answered, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("the listener this case asks is not an address with a port: %v", err)
	}
	served, restoredOverRequest, found := strings.Cut(asked, "|")
	if !found {
		t.Fatalf("the handler answered %q, want the two answers this case asks", asked)
	}
	if want := host + ":" + answered; served != want {
		t.Errorf("ServedFrom served at %s = %q, want the address the call arrived at", want, served)
	}
	if restoredOverRequest != served {
		t.Errorf("a served request answered with %q while its own socket said %q", restoredOverRequest, served)
	}
	// The worker's half: no request, only what its event carried.
	if got := ServedFrom(WithServed(context.Background(), "acme.localhost:26433")); got != "acme.localhost:26433" {
		t.Errorf("ServedFrom with a restored address = %q, want that address", got)
	}
	if got := ServedFrom(context.Background()); got != "" {
		t.Errorf("ServedFrom asked by nothing = %q, want nothing", got)
	}
	// What is not an address is never carried anywhere: the same parse
	// ServedAuthority applies to a caller's Host header.
	for _, notAnAddress := range []string{"", ":26433", "acme.localhost", "http://acme.localhost:26433/x",
		"acme.localhost:notaport", "acme.localhost:0", "acme.localhost:65536", strings.Repeat("a", 251) + ":80"} {
		if got := ServedFrom(WithServed(context.Background(), notAnAddress)); got != "" {
			t.Errorf("WithServed(%q) then ServedFrom = %q, want nothing carried", notAnAddress, got)
		}
	}
}
