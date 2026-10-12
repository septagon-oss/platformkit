package internal_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestDeliveryPreservesTheRegisteredPath(t *testing.T) {
	paths := make(chan string, 4)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths <- r.URL.Path
		if r.URL.Path != "/hooks/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), server.URL+"/hooks/", "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	rows := e.deliveries(acme, endpoint.ID)
	select {
	case path := <-paths:
		if path != "/hooks/" {
			t.Errorf("registered /hooks/ but POST reached %q; stored URL %q", path, endpoint.URL)
		}
	default:
		t.Error("the registered receiver got no request")
	}
	if len(rows) != 1 || rows[0].State != contracts.StateSent {
		t.Errorf("valid HTTPS receiver did not receive its event: %v", state(rows))
	}
}
