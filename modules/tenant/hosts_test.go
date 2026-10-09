package tenant

import "testing"

// publishedPort is the whole of what this application reads off
// server.public_host to build a link: the port the installation is published at,
// or nothing at all. An address that names none is the common case and reads as
// none, which leaves a link on its scheme's default port; an address that names a
// port has to be one a person would type, so a number out of range or not a
// number at all reads as none rather than reaching a link.
func TestPublishedPortReadsAPortAndNothingElse(t *testing.T) {
	for _, tt := range []struct {
		declared string
		want     string
	}{
		{"", ""},
		{"acme.example.com", ""},
		{"localhost", ""},
		{"acme.localhost:8443", "8443"},
		{"acme.localhost:80", "80"},
		{"acme.localhost:65535", "65535"},
		{"acme.localhost:0", ""},
		{"acme.localhost:65536", ""},
		{"acme.localhost:not-a-port", ""},
		{"acme.localhost:", ""},
		{":8080", ""},
		{"https://acme.example.com", ""},
	} {
		if got := publishedPort(tt.declared); got != tt.want {
			t.Errorf("publishedPort(%q) = %q, want %q", tt.declared, got, tt.want)
		}
	}
}
