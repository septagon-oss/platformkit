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

func TestLongExpoNoticesFitTheNotificationPayload(t *testing.T) {
	sizes := make(chan int, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sent struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			Data  struct {
				Link string `json:"link"`
			} `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Error(err)
		}
		// Even without the platform envelope, these bytes must fit its 4 KiB limit.
		size := len(sent.Title) + len(sent.Body) + len(sent.Data.Link)
		sizes <- size
		if size > 4096 {
			write(w, http.StatusOK, `{"data":{"status":"error","message":"notification payload exceeds 4096 bytes","details":{"error":"MessageTooBig"}}}`)
			return
		}
		write(w, http.StatusOK, `{"data":{"status":"ok","id":"ticket-1"}}`)
	}))
	t.Cleanup(server.Close)
	adapters := expo.New(expo.Config{BaseURL: server.URL, HTTPClient: server.Client()})
	m := pushtest.Message()
	m.Body = strings.Repeat("x", push.MaxBody*2)
	receipt, err := adapters.Send(t.Context(), device(expoToken), m)
	if err != nil || receipt.State != push.Accepted {
		t.Errorf("long notice must arrive clipped: receipt=%+v error=%v", receipt, err)
	}
	if len(sizes) != 1 {
		t.Fatalf("expected one provider request, got %d", len(sizes))
	}
	if size := <-sizes; size > 4096 {
		t.Errorf("title, body and link alone occupy %d bytes; the total notification must fit 4096", size)
	}
}
