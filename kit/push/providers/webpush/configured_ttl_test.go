package webpush_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestConfiguredLifetimeBoundsTheDefaultWebPushSend(t *testing.T) {
	s := newService(t)
	public, private := vapidKeys(t)
	lifetime := make(chan string, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lifetime <- r.Header.Get("TTL")
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	adapters := webpush.New(webpush.Config{
		PublicKey: public, PrivateKey: private, Subscriber: "mailto:push@example.test", HTTPClient: server.Client(), TTL: time.Minute,
	})
	token := fmt.Sprintf(`{"endpoint":%q,"keys":{"auth":%q,"p256dh":%q}}`, server.URL, s.auth, s.p256dh)
	m := pushtest.Message()
	m.TTL = 0
	if _, err := adapters.Send(t.Context(), device(token), m); err != nil {
		t.Fatal(err)
	}
	if got := <-lifetime; got != "60" {
		t.Errorf("configured one-minute lifetime became %s seconds on the wire", got)
	}
}
