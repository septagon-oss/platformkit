package webpush_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/webpush"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestTransportFailuresKeepSubscriptionCredentialsPrivate(t *testing.T) {
	o := observe(t)
	s := newService(t)
	public, private := vapidKeys(t)
	adapters := webpush.New(webpush.Config{
		PublicKey: public, PrivateKey: private, Subscriber: "mailto:push@example.test", HTTPClient: s.server.Client(),
	})
	const secret = "subscription-credential-not-for-logs"
	token := fmt.Sprintf(`{"endpoint":%q,"keys":{"auth":%q,"p256dh":%q}}`, s.server.URL+"/"+secret, s.auth, s.p256dh)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	receipt, err := adapters.Send(ctx, device(token), pushtest.Message())
	if !errors.Is(err, context.Canceled) || receipt != (push.Receipt{}) {
		t.Fatalf("canceled send returned receipt=%+v error=%v", receipt, err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("transport error exposes the subscription credential: %v", err)
	}
	if len(o.rec.Ended()) == 0 {
		t.Fatal("failed send produced no span")
	}
	for _, span := range o.rec.Ended() {
		for _, event := range span.Events() {
			for _, attr := range event.Attributes {
				if strings.Contains(attr.Value.Emit(), secret) {
					t.Errorf("trace event %s exposes the subscription in %s: %s", event.Name, attr.Key, attr.Value.Emit())
				}
			}
		}
	}
}
