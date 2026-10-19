package main

import (
	"bufio"
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestTheComposedPermissionStreamReleasesItsPool(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	found := false
	for i := range c.modules {
		if c.modules[i].Name == "task" {
			c.modules[i].Streams = []module.Stream{{Event: taskcontracts.EventAssigned,
				Policy: module.StreamPermission(taskcontracts.PermissionTaskRead)}}
			found = true
		}
	}
	if !found {
		t.Fatal("the reference composition has no task module")
	}
	opts := appOptions(cfg, c, app.Web)
	opts.Transport, opts.Log = memory.New(), quiet()
	var pool atomic.Pointer[db.Conn]
	authenticate := opts.Authenticate
	opts.Authenticate = func(ctx context.Context, tx db.Tx[db.Tenant], r *http.Request) (tenancy.Principal, bool, error) {
		if conn, ok := httpx.ConnFrom(ctx); ok {
			pool.Store(conn)
		}
		return authenticate(ctx, tx, r)
	}
	start(t, cfg, c.modules, opts)
	client := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+cfg.Server.Addr+"/api/v1/app/stream?topics="+taskcontracts.EventAssigned, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("permission stream = %d, want 200", res.StatusCode)
	}
	lines := bufio.NewScanner(res.Body)
	for lines.Scan() {
		if lines.Text() != "event: resync" {
			continue
		}
		conn := pool.Load()
		if conn == nil {
			t.Fatal("the stream never reached the real authenticator")
		}
		if held := conn.Stats().InUse; held != 0 {
			t.Errorf("the reference stream with the real auth authorizer holds %d pool connections; want 0", held)
		}
		return
	}
	t.Fatalf("stream ended before its opening resync: %v", lines.Err())
}
