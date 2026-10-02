package httpx

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheServedAuthorityNamesAPortOnlyWhenTheCallNamedOne is the whole of
// ServedAuthority's rule: a port is a fact about the address somebody reached us
// at, and this is the one place that reads it off a request.
func TestTheServedAuthorityNamesAPortOnlyWhenTheCallNamedOne(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		host string
		want string
	}{
		{name: "a tenant served at its name and a port", host: "acme.localhost:26433", want: "acme.localhost:26433"},
		{name: "the name is lower-cased the way the loader key is", host: "Acme.Localhost:26433", want: "acme.localhost:26433"},
		// The scheme's default port is not a fact: an event published by this call
		// carries nothing, and the link is built from the name and https.
		{name: "a tenant served at the scheme's own port", host: "acme.example.com", want: ""},
		{name: "nobody is home", host: "", want: ""},
		// A Host header is caller-supplied text. Anything that is not a name and a
		// port — a scheme, a path, a space, a second colon — is not stored.
		{name: "a whole URL", host: "http://acme.localhost:26433/x", want: ""},
		{name: "a port that is not a port", host: "acme.localhost:open", want: ""},
		{name: "no name", host: ":26433", want: ""},
		{name: "longer than a name", host: strings.Repeat("a", maxAuthority) + ":1", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ServedAuthority(&http.Request{Host: tc.host}); got != tc.want {
				t.Errorf("ServedAuthority(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
	if got := ServedAuthority(nil); got != "" {
		t.Errorf("ServedAuthority(no request) = %q, want nothing: a job is served nowhere", got)
	}
}
