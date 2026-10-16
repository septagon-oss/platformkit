package internal

import (
	"context"
	"testing"
)

// TestAPortReachesALinkOnlyForTheHostTheRequestWasServedAt is the guard beside the
// cure: the port comes from the request, the name comes from the tenant's own row,
// and nothing else about either is the caller's to choose.
func TestAPortReachesALinkOnlyForTheHostTheRequestWasServedAt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		asked string // what the request was served at, on the context
		host  string // the recipient tenant's host of record
		want  string
	}{
		{name: "the address the tenant is served at", asked: "acme.localhost:26433", host: "acme.localhost", want: "26433"},
		{name: "the same name, spelled another way", asked: "Acme.Localhost.:26433", host: "acme.localhost", want: "26433"},
		{name: "the request named no port", asked: "acme.localhost", host: "acme.localhost", want: ""},
		{name: "no request at all", asked: "", host: "acme.localhost", want: ""},
		// The name in the header is never the link's: it is checked against the row
		// and, if it is not that row, the port goes with it into the bin.
		{name: "another tenant's name", asked: "evil.example.com:26433", host: "acme.localhost", want: ""},
		{name: "this tenant's own name, with the wrong port", asked: "acme.localhost:9", host: "globex.localhost", want: ""},
		{name: "no name", asked: ":26433", host: "acme.localhost", want: ""},
		{name: "not an address", asked: "http://acme.localhost:26433/x", host: "acme.localhost", want: ""},
		// A host of record that spells its own port is what the row says it is.
		{name: "the row already names a port", asked: "acme.localhost:26433", host: "acme.localhost:9000", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithServed(context.Background(), tc.asked)
			if got := servedPort(ctx, tc.host); got != tc.want {
				t.Errorf("servedPort(%q, %q) = %q, want %q", tc.asked, tc.host, got, tc.want)
			}
		})
	}
}
