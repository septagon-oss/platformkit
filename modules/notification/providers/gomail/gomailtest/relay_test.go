package gomailtest_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail"
	"github.com/septagon-oss/platformkit/modules/notification/providers/gomail/gomailtest"
)

// TestTheRelayPresentsTheMessageTheSenderWrote is this double's own guarantee: what a
// case reads out of the relay is the text the carrier put on the wire, to the
// character, for a message long enough that the transport encodes it and folds its
// lines. The carrier writes quoted-printable, which escapes the "=" of a query string
// as "=3D" and breaks a long line with "=\r\n", so a link read off the wire is a link
// with the encoding inside it and the credential no door will spend.
//
// Every case that redeems a mailed link through this relay reads it back through
// here, so the guarantee is checked once, at the relay, with the real carrier rather
// than once per door that mails.
func TestTheRelayPresentsTheMessageTheSenderWrote(t *testing.T) {
	relay := gomailtest.Start(t)
	const token = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789"
	body := "Open http://acme.localhost:8080/app/auth/verify-email?token=" + token +
		" to confirm the account this installation just accepted. That link is the only"
	if err := gomail.New(gomail.Config{
		Host: relay.Host(), Port: relay.Port(), EnvelopeFrom: "platformkit@acme.localhost",
		Timeout: 5 * time.Second,
	}).Send(t.Context(), contracts.Message{
		To: "reader@acme.localhost", Subject: "Confirm your account", Body: body,
		HTML: "<p><a href=\"http://acme.localhost:8080/app/auth/verify-email?token=" + token + "\">confirm</a></p>",
	}); err != nil {
		t.Fatalf("send through the relay: %v", err)
	}
	sent := relay.Sent()
	if len(sent) != 1 {
		t.Fatalf("the relay kept %d messages, want the one it was sent", len(sent))
	}
	if sent[0].To != "reader@acme.localhost" {
		t.Errorf("the envelope it accepted names %q, want reader@acme.localhost", sent[0].To)
	}
	if sent[0].Body != body {
		t.Errorf("the relay presents %q for the text the sender wrote %q", sent[0].Body, body)
	}
	if at := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(sent[0].Body); len(at) != 2 ||
		at[1] != token {
		t.Errorf("the credential a case would copy out of it is %v, want %s", at, token)
	}
}
