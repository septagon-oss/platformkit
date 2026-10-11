package internal_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestANewEmailOnlyRegistrationMailsThePortThatAnsweredIt(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	router, _, _ := mountConfigured(t, conn, auth.OIDC{}, true)
	server := httptest.NewServer(router)
	defer server.Close()
	served, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(served.Host)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		server.URL+"/api/v1/public/auth/register",
		strings.NewReader(`{"email":"new.person@acme.localhost","displayName":"New Person"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = net.JoinHostPort(host, port)
	req.Header.Set("Content-Type", "application/json")
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("registration = %d, want 202", res.StatusCode)
	}
	// Each worker reads the real committed outbox payload, with no HTTP request
	// left in its context: registration creates the person, invitation mails them.
	registrationWorker(t, conn, acme)
	// Retire the event already handled. worker reads all pending rows, so leaving
	// this one there would replay registration and exercise the existing-person
	// branch before delivering the new person's invitation.
	exec(t, admin, `DELETE FROM platformkit_outbox WHERE name = 'auth.registration_requested'`)
	worker(t, conn)
	sent := mailbox.Sent()
	if len(sent) != 1 {
		t.Fatalf("registration sent %d messages, want one", len(sent))
	}
	if want := "http://" + net.JoinHostPort(host, port) + "/app/auth/reset?token="; !strings.Contains(sent[0].Body, want) {
		t.Errorf("new email-only registration lost its served address; want link starting %q, got:\n%s", want, sent[0].Body)
	}
}
