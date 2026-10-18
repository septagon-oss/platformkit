package gomail_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
)

// TestTheDeploymentsOwnAddressGoesOutUnsigned: with no tenant sender the
// message speaks as mail.from and carries no DKIM signature, since no tenant
// vouched for it and the deployment holds no key of its own.
func TestTheDeploymentsOwnAddressGoesOutUnsigned(t *testing.T) {
	r := serve(t, &relay{})
	if err := gomail.New(r.config()).Send(t.Context(), contracts.Message{
		To: "ada@example.com", Subject: "Your appointment", Body: "Come at ten.",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.Contains(r.data, "noreply@acme.example.com") {
		t.Errorf("the From header is not the deployment's address:\n%s", r.data)
	}
	if strings.Contains(strings.ToLower(r.data), "dkim-signature:") {
		t.Errorf("mail sent as the deployment carries a DKIM signature:\n%s", r.data)
	}
}

// TestAPendingTenantSenderIsNeitherSignedNorSent: a sender whose domain has
// not been verified is refused at the carrier as permanent, even when a key
// is on the row, and nothing reaches the relay.
func TestAPendingTenantSenderIsNeitherSignedNorSent(t *testing.T) {
	r := serve(t, &relay{})
	pending := sender(dkimKey(t))
	pending.Status = contracts.SenderPending
	err := gomail.New(r.config()).Send(t.Context(), contracts.Message{
		To: "ada@example.com", Subject: "s", Body: "b", Sender: pending,
	})
	if !errors.Is(err, contracts.ErrPermanent) {
		t.Fatalf("a pending sender was answered %v, want contracts.ErrPermanent", err)
	}
	if r.data != "" {
		t.Errorf("nothing should have reached the relay, got:\n%s", r.data)
	}
}
