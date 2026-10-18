package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTheDefaultMemoryStreamReceivesCommittedOutboxEvents(t *testing.T) {
	cfg, opts := compose(t)
	tenant := opts.Tenants.(fixture).tenant
	person := uuid.New()
	opts.Authenticate = func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{UserID: person}, true, nil
	}
	m := hello()
	m.Streams = []module.Stream{{Event: "hello.note_written", Policy: module.StreamAnyPrincipal()}}
	a, err := New(t.Context(), cfg, []module.Module{m}, opts)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := a.Start(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	})
	srv := httptest.NewServer(rt.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/app/stream?topics=hello.note_written", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = tenantHost
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", res.StatusCode)
	}
	lines := bufio.NewScanner(res.Body)
	opened := false
	for lines.Scan() {
		if lines.Text() == "event: resync" {
			opened = true
			break
		}
	}
	if !opened {
		t.Fatalf("stream never opened: %v", lines.Err())
	}
	tenantContext := tenancy.WithTenant(t.Context(), tenant)
	var eventID uuid.UUID
	if err := db.Run(tenantContext, rt.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := events.Publish(ctx, tx, "hello.note_written", map[string]string{"note": "private body"}); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT id FROM platformkit_outbox WHERE name = ?", "hello.note_written").Scan(&eventID).Error
	}); err != nil {
		t.Fatal(err)
	}
	// Run the worker's relay with the transport Start selected, after the stream
	// has opened. The persisted stamp proves publication completed successfully.
	if err := events.RelayApp(t.Context(), rt.conn, rt.transport, a.opts.App); err != nil {
		t.Fatal(err)
	}
	var published bool
	if err := db.Run(tenantContext, rt.conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT published_at IS NOT NULL FROM platformkit_outbox WHERE id = ?", eventID).Scan(&published).Error
	}); err != nil {
		t.Fatal(err)
	}
	if eventID == uuid.Nil || !published {
		t.Fatal("the event did not reach the relay's committed publication stamp")
	}
	for lines.Scan() {
		if lines.Text() == "id: "+eventID.String() {
			return
		}
	}
	t.Fatalf("committed outbox event %s was published through the runtime's memory transport but never reached its open stream: %v", eventID, lines.Err())
}
