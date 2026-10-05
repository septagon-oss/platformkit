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

func TestBuildRefusesAChangedRouteAuthorizationBeforeMigration(t *testing.T) {
	cfg := onOneDatabase(t)
	registrations := 0
	screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "screen", Routes: func(s httpx.Surfaces) {
			registrations++
			auth := httpx.SignedIn()
			if registrations > 1 {
				auth = httpx.Public()
			}
			httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: "/note"},
				auth, func(context.Context, *deskIn) (*deskOut, error) {
					return &deskOut{Note: "hello"}, nil
				})
		}}, nil
	})

	rt, err := pkit.NewApp("collect").Use(doors, screen).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
	}
	if err == nil {
		t.Fatal("Build accepted a route that became public after its first registration")
	}
	if !strings.Contains(err.Error(), "signed_in") || !strings.Contains(err.Error(), "public") {
		t.Errorf("refusal does not name both authorization declarations: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Build migrated %d tables before refusing the changed route authorization", got)
	}
}
