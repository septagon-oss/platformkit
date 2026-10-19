package app

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAStreamOutlivesTheServersReadTimeout serves the live door through the
// composition's own http.Server, whose read timeout bounds how long a client has
// to send its request. A stream's request was sent whole before its first frame,
// so a notice committed after that bound must still reach the open connection.
func TestAStreamOutlivesTheServersReadTimeout(t *testing.T) {
	const readTimeout = time.Second
	cfg, opts := compose(t)
	cfg.Server.ReadTimeout = readTimeout
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
	serving, stop := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- a.serve(serving, rt.Handler()) }()
	t.Cleanup(func() {
		stop()
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	listening := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", cfg.Server.Addr)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(listening) {
			t.Fatalf("the composition never listened on %s: %v", cfg.Server.Addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(t.Context(), readTimeout+10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cfg.Server.Addr+"/api/v1/app/stream?topics=hello.note_written", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = tenantHost
	req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ProtoMajor != 1 {
		t.Fatalf("stream = %s %s; want HTTP/1.1 200", res.Proto, res.Status)
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

	// The connection stays quiet past the read timeout: no frame is owed until the
	// first heartbeat, which is still in the future.
	time.Sleep(3 * readTimeout)

	tenantContext := tenancy.WithTenant(t.Context(), tenant)
	var eventID uuid.UUID
	if err := db.Run(tenantContext, rt.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := events.Publish(ctx, tx, "hello.note_written", map[string]string{"note": "body"}); err != nil {
			return err
		}
		return tx.DB().Raw("SELECT id FROM platformkit_outbox WHERE name = ?", "hello.note_written").Row().Scan(&eventID)
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.RelayApp(t.Context(), rt.conn, rt.transport, a.opts.App); err != nil {
		t.Fatal(err)
	}
	for lines.Scan() {
		if lines.Text() == "id: "+eventID.String() {
			return
		}
	}
	err = lines.Err()
	if err == nil {
		err = errors.New("the server ended the response")
	}
	t.Fatalf("a notice committed %s after the stream opened never reached it (server read timeout %s): %v",
		3*readTimeout, readTimeout, err)
}
