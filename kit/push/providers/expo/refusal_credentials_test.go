package expo_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/expo"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

func TestProviderRefusalsKeepCredentialsOutOfErrorsAndTraces(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			o := observe(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					To string `json:"to"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				detail := "cannot deliver to " + request.To
				response := map[string]any{"errors": []any{map[string]string{"message": detail}}}
				if status == http.StatusOK {
					response = map[string]any{"data": map[string]any{
						"status": "error", "message": detail, "details": map[string]string{"error": "MessageTooBig"},
					}}
				}
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(response)
			}))
			t.Cleanup(server.Close)
			adapters := expo.New(expo.Config{BaseURL: server.URL, HTTPClient: server.Client()})
			receipt, err := adapters.Send(t.Context(), device(expoToken), pushtest.Message())
			if err == nil || receipt != (push.Receipt{}) {
				t.Fatalf("provider refusal returned receipt=%+v error=%v", receipt, err)
			}
			if strings.Contains(err.Error(), expoToken) {
				t.Errorf("returned error exposes the device token: %v", err)
			}
			spans := o.rec.Ended()
			if len(spans) == 0 {
				t.Fatal("failed send produced no span")
			}
			for _, span := range spans {
				for _, event := range span.Events() {
					for _, attr := range event.Attributes {
						if strings.Contains(attr.Value.Emit(), expoToken) {
							t.Errorf("trace event %s exposes the token in %s: %s", event.Name, attr.Key, attr.Value.Emit())
						}
					}
				}
			}
		})
	}
}
