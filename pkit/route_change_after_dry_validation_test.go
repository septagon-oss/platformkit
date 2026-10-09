package pkit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBuildRefusesALiveRouteChangeBeforeOpeningTheSharedStore(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "collect", URL: "valkey://127.0.0.1:6379"}
	opened := 0
	d := buildDeployment(cfg, app.All)
	d.Caches = app.Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) {
		opened++
		return cache.Memory("collect"), nil
	}}

	registrations := 0
	screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "screen", Routes: func(s httpx.Surfaces) {
			registrations++
			path := "/note"
			if registrations == 3 {
				path = "/later"
			}
			httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: path},
				httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
					return &deskOut{Note: "a note"}, nil
				})
		}}, nil
	})

	rt, err := pkit.NewApp("collect").Use(doors, screen).Build(t.Context(), d)
	if rt != nil {
		defer rt.Close()
		t.Error("Build returned a runtime for routes that changed after dry validation")
	}
	if err == nil {
		t.Error("Build accepted routes that changed on the live registration")
	} else if !strings.Contains(err.Error(), "/note") || !strings.Contains(err.Error(), "/later") {
		t.Errorf("Build refused without naming the two route declarations: %v", err)
	}
	if registrations != 3 {
		t.Fatalf("Build registered routes %d times, want two dry and one live registration", registrations)
	}
	if opened != 0 {
		t.Errorf("Build opened the shared store %d times before refusing the live route change", opened)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Build migrated %d tables before refusing the live route change", got)
	}
}
