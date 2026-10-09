package internal_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/webhook/contracts"
)

func TestARedirectNeverReportsAnEmptyGetAsADeliveredEvent(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			type request struct {
				method string
				body   []byte
			}
			hops := make(chan request, 1)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/entry" {
					http.Redirect(w, r, "/receiver", code)
					return
				}
				body, _ := io.ReadAll(r.Body)
				hops <- request{r.Method, body}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(server.Close)
			e := newEnv(t, true, pool(server.Certificate()), nil)
			endpoint, _ := e.register(acme, uuid.New(), server.URL+"/entry", "task.assigned")
			e.fanout(acme, event(acme, "task.assigned", e.now))
			payload := e.deliveries(acme, endpoint.ID)[0].Payload
			e.drain(acme)
			rows := e.deliveries(acme, endpoint.ID)
			select {
			case hop := <-hops:
				if rows[0].State == contracts.StateSent && (hop.method != http.MethodPost || !bytes.Equal(hop.body, payload)) {
					t.Errorf("redirect %d delivered %s with %d body bytes, but log says %s status=%d; want the signed POST body or a non-success outcome",
						code, hop.method, len(hop.body), rows[0].State, rows[0].StatusCode)
				}
			default:
				if rows[0].State == contracts.StateSent {
					t.Error("redirect was not delivered but log says sent")
				}
			}
		})
	}
}
