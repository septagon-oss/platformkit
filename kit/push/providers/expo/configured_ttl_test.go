package expo_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/expo"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestConfiguredLifetimeBoundsTheDefaultExpoSend(t *testing.T) {
	s := newService(t)
	adapters := expo.New(expo.Config{BaseURL: s.server.URL, HTTPClient: s.server.Client(), TTL: time.Minute})
	m := pushtest.Message()
	m.TTL = 0
	if _, err := adapters.Send(t.Context(), device(expoToken), m); err != nil {
		t.Fatal(err)
	}
	var sent struct {
		TTL int64 `json:"ttl"`
	}
	if err := json.Unmarshal([]byte(s.lastBody()), &sent); err != nil {
		t.Fatal(err)
	}
	if sent.TTL != 60 {
		t.Errorf("configured one-minute lifetime became %d seconds on the wire (port default is %s)", sent.TTL, push.MaxTTL)
	}
}
