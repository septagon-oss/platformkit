package internal_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestTheSignatureHeaderIsStandardWebhooksSpelling: a receiver that uses a
// Standard Webhooks library reads webhook-signature as a space-delimited list
// of "<version>,<base64>" entries and accepts the first v1 entry whose HMAC
// matches. The check below is that algorithm, as the reference Go library
// (github.com/standard-webhooks/standard-webhooks/libraries/go) writes it.
func TestTheSignatureHeaderIsStandardWebhooksSpelling(t *testing.T) {
	to := newReceiver(t, http.StatusOK)
	e := newEnv(t, true, pool(to.server.Certificate()), nil)
	_, secret := e.register(acme, uuid.New(), to.server.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	got := to.last(t)

	key, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "whsec_"))
	if err != nil {
		t.Fatalf("the secret %q is not whsec_ + base64: %v", secret, err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(got.id + "." + got.timestamp + "." + string(got.body)))
	want := []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	for _, versioned := range strings.Split(got.signature, " ") {
		parts := strings.Split(versioned, ",")
		if len(parts) < 2 || parts[0] != "v1" {
			continue
		}
		if hmac.Equal([]byte(parts[1]), want) {
			return
		}
	}
	t.Fatalf("no v1,<signature> entry in webhook-signature %q verifies", got.signature)
}
