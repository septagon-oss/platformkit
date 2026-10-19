package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// streamingComposition boots a whole application whose module streams one event and
// whose live door is open, and hands back its Runtime, whose tenant it is, how many
// transports the composition dialed, and the connection's lines from its opening
// resync on.
//
// The dial count is what lets the cases below see the composition from outside: a
// connection watching a transport other than the one the composition's own relay
// publishes the outbox onto opens, says resync, and is told nothing forever — which
// is invisible from inside either half and one number from outside.
func streamingComposition(t *testing.T) (*Runtime, tenancy.Tenant, *atomic.Int64, <-chan string) {
	t.Helper()
	cfg, opts := compose(t)
	tenant := opts.Tenants.(fixture).tenant
	opts.Authenticate = func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		return tenancy.Principal{UserID: uuid.New()}, true, nil
	}
	dials := new(atomic.Int64)
	opts.Transports.Memory = func() events.Transport {
		dials.Add(1)
		return memory.New()
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
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
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
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d, want 200", res.StatusCode)
	}
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	for {
		line, ok := <-lines
		if !ok {
			t.Fatal("the live connection ended before its opening resync")
		}
		if line == "event: resync" {
			return rt, tenant, dials, lines
		}
	}
}

// relayedEvent writes one event into the outbox and moves it with the relay this
// Runtime works with, which is the worker half's own job, and answers with its id.
// The publication stamp is read back first: a relay that left the row pending would
// otherwise leave the case below waiting on a notice nobody published, and say so.
func relayedEvent(t *testing.T, rt *Runtime, tenant tenancy.Tenant) uuid.UUID {
	t.Helper()
	ctx := tenancy.WithTenant(t.Context(), tenant)
	var id uuid.UUID
	if err := db.Run(ctx, rt.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := events.Publish(ctx, tx, "hello.note_written", map[string]string{"note": "private body"}); err != nil {
			return err
		}
		var written string
		if err := tx.DB().Raw("SELECT id FROM platformkit_outbox WHERE name = ?", "hello.note_written").Row().Scan(&written); err != nil {
			return err
		}
		var err error
		if id, err = uuid.Parse(written); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.RelayApp(t.Context(), rt.conn, rt.transport, rt.app.opts.App); err != nil {
		t.Fatal(err)
	}
	var stamped bool
	if err := db.Run(ctx, rt.conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT published_at IS NOT NULL FROM platformkit_outbox WHERE id = ?", id).Scan(&stamped).Error
	}); err != nil {
		t.Fatal(err)
	}
	if !stamped {
		t.Fatal("the relay reached no committed publication stamp for the event")
	}
	return id
}

func TestTheLiveDoorDeliversTheOutboxEventItsOwnRelayPublished(t *testing.T) {
	rt, tenant, _, lines := streamingComposition(t)
	id := relayedEvent(t, rt, tenant)
	for {
		line, ok := <-lines
		if !ok {
			t.Fatalf("the live connection ended without the notice for the relayed event %s", id)
		}
		if line == "id: "+id.String() {
			return
		}
	}
}

// The count is the whole case: one process, one transport, whatever halves of it
// serve. A second dial means the door and the relay are two buses apart, and the
// in-process transport delivers to nobody across that gap.
func TestACompositionDialsOneTransportForItsRelayAndItsLiveDoor(t *testing.T) {
	_, _, dials, _ := streamingComposition(t)
	if got := dials.Load(); got != 1 {
		t.Fatalf("this composition's boot and live connection dialled %d event transports, want the one its relay publishes on", got)
	}
}
