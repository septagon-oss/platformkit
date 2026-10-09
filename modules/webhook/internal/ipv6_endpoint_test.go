package internal_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestDeliveryPreservesAnIPv6EndpointPort(t *testing.T) {
	listener, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	_ = server.Listener.Close()
	server.Listener = listener
	server.StartTLS()
	t.Cleanup(server.Close)
	e := newEnv(t, true, pool(server.Certificate()), nil)
	endpoint, _ := e.register(acme, uuid.New(), server.URL+"/hook", "task.assigned")
	e.fanout(acme, event(acme, "task.assigned", e.now))
	e.drain(acme)
	rows := e.deliveries(acme, endpoint.ID)
	if len(rows) != 1 || rows[0].State != contracts.StateSent {
		t.Fatalf("registered %q, stored %q, delivery %v", server.URL+"/hook", endpoint.URL, state(rows))
	}
}
