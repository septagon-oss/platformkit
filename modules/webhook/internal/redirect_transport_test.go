package internal_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestAWebhookRedirectCannotSendTheEventOverPlainHTTP(t *testing.T) {
	var leaked atomic.Int64
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 && r.Header.Get("webhook-signature") != "" {
			leaked.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(plain.Close)
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(tls.Close)
	e := newEnv(t, true, pool(tls.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), tls.URL, "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	if got := leaked.Load(); got != 0 {
		t.Fatalf("HTTPS endpoint leaked the signed event over plain HTTP %d time(s); log %v", got, state(e.deliveries(acme, endpoint.ID)))
	}
}
