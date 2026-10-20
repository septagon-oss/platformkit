package webpush_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestSubscriptionPublicKeyMustBeOnTheCurve(t *testing.T) {
	s := newService(t)
	public, private := vapidKeys(t)
	adapters := webpush.New(webpush.Config{
		PublicKey: public, PrivateKey: private, Subscriber: "mailto:push@example.test", HTTPClient: s.server.Client(),
	})
	// An uncompressed point of the right length still cannot address a browser
	// when its coordinates are not on P-256. No provider can correct that input.
	point := make([]byte, 65)
	point[0] = 4
	raw, err := json.Marshal(map[string]any{
		"endpoint": s.server.URL,
		"keys":     map[string]string{"auth": s.auth, "p256dh": base64.RawURLEncoding.EncodeToString(point)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adapters.Get(push.PlatformWeb).Validate(string(raw)); !errors.Is(err, push.ErrMalformed) {
		t.Errorf("off-curve public key passed validation: %v", err)
	}
	receipt, err := adapters.Send(t.Context(), device(string(raw)), pushtest.Message())
	if !errors.Is(err, push.ErrMalformed) || receipt != (push.Receipt{}) {
		t.Errorf("off-curve public key: receipt=%+v error=%v; want ErrMalformed and no receipt", receipt, err)
	}
	if s.getRequests() != 0 {
		t.Error("unusable subscription reached the push service")
	}
}
