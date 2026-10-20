package expo_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/expo"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestTruncatedProviderRefusalsKeepCredentialFragmentsPrivate(t *testing.T) {
	for _, credential := range []string{"device token", "access token"} {
		t.Run(credential, func(t *testing.T) {
			o := observe(t)
			secret := "credential-fragment-that-must-stay-private-" + strings.Repeat("x", 240)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte("cannot deliver using " + secret))
			}))
			t.Cleanup(server.Close)
			cfg := expo.Config{BaseURL: server.URL, HTTPClient: server.Client()}
			token := expoToken
			if credential == "device token" {
				token = secret
			} else {
				cfg.AccessToken = secret
			}
			receipt, err := expo.New(cfg).Send(t.Context(), device(token), pushtest.Message())
			if err == nil || receipt != (push.Receipt{}) {
				t.Fatalf("provider refusal: receipt=%+v error=%v", receipt, err)
			}
			if strings.Contains(err.Error(), secret[:48]) {
				t.Errorf("returned error exposes a %s prefix: %v", credential, err)
			}
			if len(o.rec.Ended()) == 0 {
				t.Fatal("failed send produced no span")
			}
			for _, span := range o.rec.Ended() {
				for _, event := range span.Events() {
					for _, attr := range event.Attributes {
						if strings.Contains(attr.Value.Emit(), secret[:48]) {
							t.Errorf("trace %s exposes a %s prefix in %s", event.Name, credential, attr.Key)
						}
					}
				}
			}
		})
	}
}
