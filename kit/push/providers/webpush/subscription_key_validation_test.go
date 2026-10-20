package webpush_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestUnusableSubscriptionKeysAreMalformedBeforeSending(t *testing.T) {
	s := newService(t)
	public, private := vapidKeys(t)
	adapters := webpush.New(webpush.Config{
		PublicKey: public, PrivateKey: private, Subscriber: "mailto:push@example.test", HTTPClient: s.server.Client(),
	})
	for _, key := range []string{"auth", "p256dh"} {
		t.Run(key, func(t *testing.T) {
			keys := map[string]string{"auth": s.auth, "p256dh": s.p256dh}
			keys[key] = "%%%"
			raw, err := json.Marshal(map[string]any{"endpoint": s.server.URL, "keys": keys})
			if err != nil {
				t.Fatal(err)
			}
			if err := adapters.Get(push.PlatformWeb).Validate(string(raw)); !errors.Is(err, push.ErrMalformed) {
				t.Errorf("unusable %s key passed registration validation: %v", key, err)
			}
			receipt, err := adapters.Send(t.Context(), device(string(raw)), pushtest.Message())
			if !errors.Is(err, push.ErrMalformed) || receipt != (push.Receipt{}) {
				t.Errorf("unusable %s key: receipt=%+v error=%v; want ErrMalformed, not a retryable outage", key, receipt, err)
			}
			if s.getRequests() != 0 {
				t.Error("malformed subscription reached the push service")
			}
		})
	}
}
