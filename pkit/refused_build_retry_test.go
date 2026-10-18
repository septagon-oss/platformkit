package pkit_test

import (
	"context"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestARefusedLiveRouteCanBeCorrectedAndBuilt(t *testing.T) {
	cfg := onOneDatabase(t)
	registrations := 0
	unstable := true
	screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "screen", Routes: func(s httpx.Surfaces) {
			registrations++
			path := "/note"
			if unstable && registrations == 4 {
				path = "/later"
			}
			httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: path},
				httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
					return &deskOut{Note: "a note"}, nil
				})
		}}, nil
	})
	a := pkit.NewApp("collect").Use(doors, screen)
	d := buildDeployment(cfg, app.All)

	rt, err := a.Build(t.Context(), d)
	if rt != nil {
		defer rt.Close()
		t.Fatal("Build returned a runtime for inconsistent routes")
	}
	if err == nil || !strings.Contains(err.Error(), "/note") || !strings.Contains(err.Error(), "/later") {
		t.Fatalf("the fourth registration did not refuse its route change: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Fatalf("the refused build migrated %d tables", got)
	}

	unstable = false
	rt, err = a.Build(t.Context(), d)
	if err != nil {
		t.Fatalf("the corrected composition could not build after a refusal with no effects: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
