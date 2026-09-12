package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func TestSignInRetainsTheGuardedPageAndQuery(t *testing.T) {
	for _, tt := range []struct {
		name, path, query, want string
	}{
		{"page", "/albums", "", "/login?next=%2Falbums"},
		{"filters", "/albums", "sort=recent&page=2", "/login?next=%2Falbums%3Fsort%3Drecent%26page%3D2"},
		{"query encoding", "/albums", "q=a%2Bb+%26+c&tag=one&tag=two", "/login?next=%2Falbums%3Fq%3Da%252Bb%2B%2526%2Bc%26tag%3Done%26tag%3Dtwo"},
		{"nested destination stays data", "/albums", "next=https%3A%2F%2Fevil.example", "/login?next=%2Falbums%3Fnext%3Dhttps%253A%252F%252Fevil.example"},
		{"network path", "//evil.example", "", ""},
		{"browser normalized authority", `/\evil.example`, "", ""},
		{"absolute destination", "https://evil.example", "", ""},
		{"relative destination", "albums", "", ""},
		{"empty destination", "", "", ""},
		{"control character", "/albums\r\nLocation: https://evil.example", "", ""},
		{"unsafe query", "/albums", "q=\r\nLocation: https://evil.example", ""},
		{"login loop", "/login", "next=%2Falbums", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			op := &huma.Operation{}
			SignIn(op, "/login")
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://tenant.test/", nil)
			req.URL.Path, req.URL.RawQuery = tt.path, tt.query
			req.Header.Set("Accept", "text/html")
			got, redirect := signInFor(humago.NewContext(op, req, httptest.NewRecorder()))
			if got != tt.want || redirect != (tt.want != "") {
				t.Errorf("sign-in destination = %q, %v; want %q, %v", got, redirect, tt.want, tt.want != "")
			}
		})
	}
}

func TestSignInRedirectRequiresALocalHTMLRead(t *testing.T) {
	for _, tt := range []struct {
		name, method, accept, signin string
	}{
		{"write", http.MethodPost, "text/html", "/login"},
		{"JSON read", http.MethodGet, "application/json", "/login"},
		{"undeclared form", http.MethodGet, "text/html", ""},
		{"offsite form", http.MethodGet, "text/html", "https://evil.example/login"},
		{"network form", http.MethodGet, "text/html", "//evil.example/login"},
		{"browser normalized form", http.MethodGet, "text/html", `/\evil.example/login`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			op := &huma.Operation{}
			SignIn(op, tt.signin)
			req := httptest.NewRequestWithContext(t.Context(), tt.method, "http://tenant.test/albums?sort=recent", nil)
			req.Header.Set("Accept", tt.accept)
			if got, redirect := signInFor(humago.NewContext(op, req, httptest.NewRecorder())); got != "" || redirect {
				t.Errorf("ineligible request redirects to %q", got)
			}
		})
	}
}

func TestSignInPreservesEscapedPathsAndDeclaredQuery(t *testing.T) {
	for _, tt := range []struct{ target, signin, want string }{
		{"/albums/a%2Fb?view=grid", "/login", "/login?next=%2Falbums%2Fa%252Fb%3Fview%3Dgrid"},
		{"/albums/100%25", "/login", "/login?next=%2Falbums%2F100%2525"},
		{"/albums/new%20collection", "/login", "/login?next=%2Falbums%2Fnew%2520collection"},
		{"/albums?sort=recent", "/login?locale=pt&next=old#form", "/login?locale=pt&next=%2Falbums%3Fsort%3Drecent#form"},
		{"/login?next=%2Falbums", "/login?locale=pt", ""},
	} {
		t.Run(tt.target+" via "+tt.signin, func(t *testing.T) {
			op := &huma.Operation{}
			SignIn(op, tt.signin)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://tenant.test"+tt.target, nil)
			req.Header.Set("Accept", "text/html")
			got, redirect := signInFor(humago.NewContext(op, req, httptest.NewRecorder()))
			if got != tt.want || redirect != (tt.want != "") {
				t.Errorf("sign-in destination = %q, %v; want %q, %v", got, redirect, tt.want, tt.want != "")
			}
		})
	}
}
