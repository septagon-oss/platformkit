package httpx

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTheServedAuthorityNamesAPortOnlyWhenTheCallNamedOne is the whole of
// ServedAuthority's rule: a port is a fact about the address somebody reached us
// at, and this is the one place that reads it off a request.
//
// Half the cases are asked of a live listener, because the port's provenance is the
// property: a port read off the Host header is whatever the caller wrote, and a
// port read off the socket is only ever a port this process answered on. The rest
// are asked of requests no transport ever carried, which is how a value that is not
// an address, and a call with no socket behind it, are both refused a port.
func TestTheServedAuthorityNamesAPortOnlyWhenTheCallNamedOne(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ServedAuthority(r))
	}))
	defer server.Close()
	_, answered, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("the listener this case asks is not an address with a port: %v", err)
	}

	cases := []struct {
		name string
		host string
		want string
	}{
		{name: "a tenant served at its name and a port", host: "acme.localhost:" + answered, want: "acme.localhost:" + answered},
		{name: "the name is lower-cased the way the loader key is", host: "Acme.Localhost:" + answered, want: "acme.localhost:" + answered},
		// The scheme's default port is not a fact: an event published by this call
		// carries nothing, and the link is built from the name and https. A proxy in
		// front of this process would be answering on 443 and serving on its own
		// port; the port the socket says is exactly the one that must not travel.
		{name: "a tenant served at the scheme's own port", host: "acme.example.com", want: ""},
		// Host is text the caller wrote. The call was answered at `answered`, and
		// that is the port the address it is served at names: a caller that aimed a
		// request at some other port gets nothing in the mail but the port of record.
		{name: "a port the caller chose and we do not serve", host: "acme.localhost:7", want: "acme.localhost:" + answered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Host = tc.host
			res, err := server.Client().Do(req)
			if err != nil {
				t.Fatalf("a call at %q never reached the listener: %v", tc.host, err)
			}
			defer res.Body.Close()
			got, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("ServedAuthority of a call at %q = %q, want %q", tc.host, got, tc.want)
			}
		})
	}

	// A Host header is caller-supplied text. Anything that is not a name and a
	// port — a scheme, a path, a space, a second colon — is not stored, whether or
	// not a socket stands behind the call.
	for _, host := range []string{
		"http://acme.localhost:26433/x", "acme.localhost:open", ":26433", "",
		strings.Repeat("a", maxAuthority) + ":1",
	} {
		if got := ServedAuthority(&http.Request{Host: host}); got != "" {
			t.Errorf("ServedAuthority(%q) = %q, want nothing: not an address, or a call no socket carried", host, got)
		}
	}
	// A named port needs a socket that names it too. Everything below this line is
	// a request no transport ever carried — a job, a replay, a handler driven in
	// process — and no port of anybody's is a fact about it.
	if got := ServedAuthority(&http.Request{Host: "acme.localhost:26433"}); got != "" {
		t.Errorf("ServedAuthority of a call with no socket = %q, want nothing: no port was answered on", got)
	}
	if got := ServedAuthority(nil); got != "" {
		t.Errorf("ServedAuthority(no request) = %q, want nothing: a job is served nowhere", got)
	}
}
