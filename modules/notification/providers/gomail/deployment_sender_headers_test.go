package gomail_test

import (
	"net/mail"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
)

func TestDeploymentIdentityIsInTheHeadersAndEnvelope(t *testing.T) {
	r := serve(t, &relay{})
	cfg := r.config()
	cfg.EnvelopeFrom = "Deployment Relay <relay@example.org>"
	if err := gomail.New(cfg).Send(t.Context(), contracts.Message{
		To: "ada@example.com", Subject: "Notice", Body: "Tenant: notifications@acme.example.com",
	}); err != nil {
		t.Fatal(err)
	}
	<-r.accepted
	message, err := mail.ReadMessage(strings.NewReader(r.data))
	if err != nil {
		t.Fatal(err)
	}
	from, err := message.Header.AddressList("From")
	if err != nil {
		t.Fatal(err)
	}
	if len(from) != 1 || from[0].Name != "Deployment Relay" || from[0].Address != "relay@example.org" {
		t.Fatalf("From = %v, want the deployment's name and address", from)
	}
	if r.envelope != "relay@example.org" || message.Header.Get("DKIM-Signature") != "" {
		t.Fatalf("envelope = %q, signature = %q", r.envelope, message.Header.Get("DKIM-Signature"))
	}
}
