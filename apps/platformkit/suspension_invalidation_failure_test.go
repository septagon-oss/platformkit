package main

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

type invalidationUnavailable struct{ cache.Cache }

type sharedHandle struct{ cache.Cache }

func (sharedHandle) Close() error { return nil }

func (invalidationUnavailable) Move(context.Context, cache.Scope) error {
	return errors.New("the shared store did not accept the invalidation")
}

func TestASuspensionCannotReportCompleteSuccessWhenInvalidationFails(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "susp-failed-move", URL: "valkey://127.0.0.1:6379"}
	backend := cache.MemoryBackend()
	t.Cleanup(func() { _ = backend.Close() })
	build := func(_ context.Context, c config.Cache) (cache.Cache, error) {
		return sharedHandle{cache.MemoryNamed(c.App, backend)}, nil
	}
	first := compose(cfg)
	start(t, cfg, first.modules, app.Options{
		Tenants: first.tenants, Authorize: first.auth, Entitle: first.plans, Authenticate: first.auth.Authenticate,
		Role: app.All, Transport: memory.New(), Log: quiet(),
		Caches: app.Caches{Valkey: func(ctx context.Context, c config.Cache) (cache.Cache, error) {
			store, err := build(ctx, c)
			return invalidationUnavailable{store}, err
		}},
	})
	other := cfg
	other.Server.Addr = freeAddr(t)
	second := compose(other)
	start(t, other, second.modules, app.Options{
		Tenants: second.tenants, Authorize: second.auth, Entitle: second.plans, Authenticate: second.auth.Authenticate,
		Role: app.Web, Transport: memory.New(), Log: quiet(), Caches: app.Caches{Valkey: build},
	})

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("create the tenant = %d %s, want 201", code, body)
	}
	id := field(t, body, "id")
	for _, c := range []config.Config{cfg, other} {
		if code, body = do(t, c, nil, http.MethodGet, globexHost, tasksPath, ""); code != http.StatusForbidden {
			t.Fatalf("warm the served host on %s = %d %s, want 403", c.Server.Addr, code, body)
		}
	}

	// The write commits, but this process's store connection refuses the Move.
	// The other process can still read its old entry. Either the response must
	// say the outcome is uncertain, or that old entry must cease to be served.
	suspendCode, suspendBody := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath+"/"+id+"/suspend", "")
	if suspendCode != http.StatusOK && suspendCode != http.StatusAccepted && suspendCode != http.StatusServiceUnavailable {
		t.Fatalf("suspend reached an unexpected outcome: %d %s", suspendCode, suspendBody)
	}
	servedCode, servedBody := do(t, other, nil, http.MethodGet, globexHost, tasksPath, "")
	if suspendCode == http.StatusOK && servedCode == http.StatusForbidden {
		t.Errorf("suspend reported %d %s while the second process still served the host: %d %s", suspendCode, suspendBody, servedCode, servedBody)
	}
}
