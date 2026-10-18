package webpush_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestNotificationIdentifiersProduceCarryableWebPushTopics(t *testing.T) {
	keys := newService(t)
	public, private := vapidKeys(t)
	topics := make(chan string, 4)
	valid := regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		topic := r.Header.Get("Topic")
		topics <- topic
		// RFC 8030 section 5.4 requires a 400 for an invalid topic.
		if !valid.MatchString(topic) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)
	adapters := webpush.New(webpush.Config{
		PublicKey: public, PrivateKey: private, Subscriber: "mailto:push@example.test", HTTPClient: server.Client(),
	})
	token := fmt.Sprintf(`{"endpoint":%q,"keys":{"auth":%q,"p256dh":%q}}`, server.URL, keys.auth, keys.p256dh)
	m := pushtest.Message()
	m.Tag = "4b4b4b4b-4b4b-4b4b-4b4b-4b4b4b4b4b4b"
	if err := m.Validate(); err != nil {
		t.Fatalf("notification id must be a valid port tag: %v", err)
	}
	for range 2 {
		receipt, err := adapters.Send(t.Context(), device(token), m)
		if err != nil || receipt.State != push.Delivered {
			t.Errorf("notification-id tag was not delivered: receipt=%+v error=%v", receipt, err)
		}
	}
	if len(topics) != 2 {
		t.Fatalf("two sends reached the service %d times", len(topics))
	}
	first, second := <-topics, <-topics
	if !valid.MatchString(first) || first != second {
		t.Errorf("topic must be valid and stable across retries: first=%q second=%q", first, second)
	}
}
