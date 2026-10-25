package httpx_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
)

func TestReplaysKeepTheirOriginalIntegerRepresentation(t *testing.T) {
	for _, receipts := range []bool{false, true} {
		for _, exact := range []bool{false, true} {
			t.Run(fmt.Sprintf("receipts=%t/exact=%t", receipts, exact), func(t *testing.T) {
				_, _, f := setup(t)
				f.signedIn()
				f.allow = true
				instant := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
				opts := httpx.Options{Cache: cache.Memory("int64-replay"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate, Idempotency: idempotency.Memory(func() time.Time { return instant }), Clock: func() time.Time { return instant }}
				if receipts {
					opts.Operations = idempotency.PostgresOperations()
				}
				api, router := httpx.New(opts)
				runs := 0
				httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "integer-replay", Method: "POST", Path: "/integer-replay"}, httpx.Permission("probe:write"),
					func(ctx context.Context, in *struct {
						Body struct {
							Amount int64 `json:"amount"`
						}
					}) (*struct{ Body map[string]int64 }, error) {
						runs++
						tx, _ := httpx.TxFrom(ctx)
						if err := events.Publish(ctx, tx, "probe.integer", struct{ Amount int64 }{in.Body.Amount}); err != nil {
							return nil, err
						}
						return &struct{ Body map[string]int64 }{map[string]int64{"result": in.Body.Amount}}, nil
					})
				firstMode, otherMode := "application/json", "application/vnd.platformkit.int64+json"
				if exact {
					firstMode, otherMode = otherMode, firstMode
				}
				post := func(accept, body string) *httptest.ResponseRecorder {
					r := httptest.NewRequest("POST", "http://"+host+at(api, "/integer-replay"), strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("Accept", accept)
					r.Header.Set(httpx.IdempotencyKeyHeader, "integer-replay-key")
					r.Header.Set("Authorization", "Bearer present")
					w := httptest.NewRecorder()
					router.ServeHTTP(w, r)
					return w
				}
				const body = `{"amount":9007199254740993}`
				first := post(firstMode, body)
				integerStatus(t, first, 200)
				for range 2 {
					retry := post(firstMode, body)
					if retry.Code != 200 || retry.Body.String() != first.Body.String() || retry.Header().Get("Content-Type") != firstMode {
						t.Fatalf("replay = %d %s", retry.Code, retry.Body.String())
					}
				}
				mismatch := post(otherMode, body)
				integerStatus(t, mismatch, 409)
				integerStatus(t, post(firstMode, `{"amount":"9007199254740993"}`), 422)
				if recovered := post(firstMode, body); recovered.Code != 200 || recovered.Body.String() != first.Body.String() {
					t.Fatal("original answer lost")
				}
				f.allow = false
				integerStatus(t, post(firstMode, body), 403)
				if runs != 1 {
					t.Fatalf("handler ran %d times", runs)
				}
			})
		}
	}
}
