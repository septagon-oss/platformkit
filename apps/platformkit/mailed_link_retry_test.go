package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

func TestMailedLinkRetriesPreserveTheCredentialAndStopAfterSuccess(t *testing.T) {
	const path = "/api/v1/auth/password/reset"
	const body = `{"token":"mailed-credential","new":"a chosen passphrase"}`
	attempts := make(chan string, 20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.Host != acmeHost || r.URL.Path != path ||
			r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("redemption changed its tenant or request: %s %s %s %s",
				r.Method, r.Host, r.URL.Path, r.Header.Get("Content-Type"))
		}
		attempts <- string(got)
		if len(attempts) < 3 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := config.Config{Server: config.Server{Addr: strings.TrimPrefix(server.URL, "http://")}}
	redeemMailedLink(t, cfg, server.Client(), acmeHost, path, body)
	if len(attempts) != 3 {
		t.Fatalf("redemption made %d attempts, want two pending refusals and one success", len(attempts))
	}
	for range 3 {
		if got := <-attempts; got != body {
			t.Errorf("redemption changed the mailed credential or password: %s", got)
		}
	}
}
