package app

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnOperatorStreamRefusesATenantPrincipal(t *testing.T) {
	cfg, opts := compose(t)
	person := opts.Tenants.(fixture).tenant.ID
	var authenticated atomic.Bool
	opts.Authenticate = func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
		authenticated.Store(true)
		return tenancy.Principal{UserID: person, Roles: []string{"reader"}}, true, nil
	}
	m := hello()
	m.Permissions[0].Operator = true
	m.Streams = []module.Stream{{Event: "hello.note_written", Policy: module.StreamPermission("note:write")}}
	a, err := New(t.Context(), cfg, []module.Module{m}, opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-stopped; err != nil {
			t.Errorf("Run: %v", err)
		}
	})
	waitFor(t, cfg.Server.Addr, stopped)
	rctx, end := context.WithTimeout(t.Context(), 5*time.Second)
	defer end()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet,
		"http://"+cfg.Server.Addr+"/api/v1/app/stream?topics=hello.note_written", nil)
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
	if !authenticated.Load() {
		t.Fatal("the request never reached authentication")
	}
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("operator-permission stream at a non-operator tenant = %d; want 403 before consulting the tenant's authorizer", res.StatusCode)
	}
}
