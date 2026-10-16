package pkit_test

import (
	"context"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// A route callback may retain an idempotent mount guard across the dry and live
// registration. If the two registrations disagree, Build must settle that
// disagreement before it migrates the database.
func TestRouteRegistrationRefusalLeavesNoMigration(t *testing.T) {
	cfg := onOneDatabase(t)
	var mounted sync.Once
	screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "screen", Routes: func(s httpx.Surfaces) {
			mounted.Do(func() {
				httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: "/note"},
					httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
						return &deskOut{Note: "hello"}, nil
					})
			})
		}}, nil
	})
	rt, err := pkit.NewApp("collect").Use(doors, screen).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); err != nil && got != 0 {
		t.Fatalf("a refused route registration left %d migrated tables: %v", got, err)
	}
	if (err == nil) != (rt != nil) {
		t.Fatalf("Build returned an inconsistent runtime and refusal: runtime=%v, err=%v", rt, err)
	}
}
