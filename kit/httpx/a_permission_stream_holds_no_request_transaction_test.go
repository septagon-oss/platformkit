package httpx_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/stream"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// shortTxAuthority answers the stream's re-check the way its port asks: inside one
// short transaction of its own, opened with db.Run on the context it was handed.
type shortTxAuthority struct{ conn *db.Conn }

func (a shortTxAuthority) Renew(ctx context.Context, _ tenancy.Tenant, _ *http.Request, _ []string) (stream.Renewal, error) {
	err := db.Run(ctx, a.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("SELECT 1").Error
	})
	return stream.Renewal{SignedIn: true}, err
}

// TestAPermissionStreamHoldsNoRequestTransaction opens a live connection on a
// topic whose policy names a grant, over the streamed-response door, and checks
// that once the opening frames are on the wire no pool connection is in use: the
// connect-time check ran in a transaction of its own and committed, and the
// request's transaction stays closed for as long as the stream runs.
func TestAPermissionStreamHoldsNoRequestTransaction(t *testing.T) {
	for name, policy := range map[string]module.StreamPolicy{
		"any principal": module.StreamAnyPrincipal(),
		"permission":    module.StreamPermission("task:read"),
	} {
		t.Run(name, func(t *testing.T) {
			api, router, f := setup(t)
			f.principal = &tenancy.Principal{UserID: uuid.New()}
			hub := stream.New(stream.Deps{
				Transport: memory.New(),
				Registry: stream.NewRegistry([]module.Module{{Name: "task", Events: []string{"task.closed"},
					Streams: []module.Stream{{Event: "task.closed", Policy: policy}}}}),
				Authority: shortTxAuthority{conn: f.app},
				Limits:    stream.DefaultLimits(),
			})
			httpx.Register(api.Surfaces(probe).App, huma.Operation{
				OperationID: "live", Method: http.MethodGet, Path: "/live",
				Extensions: map[string]any{httpx.StreamedResponseExtension: true},
			}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
				tenant, _ := tenancy.FromContext(ctx)
				principal, _ := tenancy.PrincipalFrom(ctx)
				r, _ := httpx.RequestFrom(ctx)
				req := stream.Request{Tenant: tenant, Principal: principal, HTTP: r, Topics: []string{"task.closed"}}
				return &huma.StreamResponse{Body: func(hctx huma.Context) {
					w, _ := hctx.BodyWriter().(http.ResponseWriter)
					if err := hub.Serve(hctx.Context(), w, req); err != nil {
						hctx.SetStatus(http.StatusInternalServerError)
						return
					}
					hctx.SetStatus(http.StatusOK)
				}}, nil
			})

			srv := httptest.NewServer(router)
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+at(api, "/live"), nil)
			req.Host = host
			req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("the stream answered %d", res.StatusCode)
			}
			lines := bufio.NewScanner(res.Body)
			for lines.Scan() && !strings.HasPrefix(lines.Text(), "event: resync") {
			}
			if lines.Err() != nil {
				t.Fatalf("reading the stream: %v", lines.Err())
			}
			time.Sleep(100 * time.Millisecond)
			if inUse := f.app.Stats().InUse; inUse != 0 {
				t.Errorf("a live %s stream holds %d pool connections after its opening frames; want 0", name, inUse)
			}
		})
	}
}
