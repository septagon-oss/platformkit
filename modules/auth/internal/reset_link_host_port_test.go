package internal_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
)

// A caller can choose the Host header independently of the listener it reaches.
// The reset mail must lead to the address that answered the request, even when
// the caller named a different port on this tenant's host.
func TestResetMailDoesNotTrustAnUnservedHostPort(t *testing.T) {
	router, conn, _ := mount(t, auth.OIDC{})
	person(t, conn, "ada@acme.localhost")
	server := httptest.NewServer(router)
	defer server.Close()

	served, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, servedPort, err := net.SplitHostPort(served.Host)
	if err != nil {
		t.Fatal(err)
	}
	const chosenPort = "7"
	if servedPort == chosenPort {
		t.Fatal("the fixture unexpectedly serves on the caller's chosen port")
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/public/auth/password/forgot",
		strings.NewReader(`{"email":"ada@acme.localhost"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host + ":" + chosenPort
	req.Header.Set("Content-Type", "application/json")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("asking for the link = %d, want 200", res.StatusCode)
	}

	worker(t, conn)
	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("the worker sent %d reset mails, want one", len(sent))
	}
	if want := "http://" + host + ":" + servedPort + "/app/auth/reset?token="; !strings.Contains(sent[0].Body, want) {
		t.Errorf("the application served at %s but mailed a link to the caller's port:\n%s", want, sent[0].Body)
	}
}
