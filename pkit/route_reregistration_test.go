package pkit_test

// The dry registration and the live one are two calls of the same callback, and
// kit/module.Module.Routes never required a callback to answer the same way twice.
// A module that mounts a different route the second time is the case no route gate
// refuses: both registrations mount something on the workspace, so every gate
// answers about a composition that is not the one that would serve. Build refuses
// it, and refuses it with the schema still empty, because the second registration
// is read before the migration rather than after it.

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestRouteRegistrationThatChangesAfterTheGatesIsRefused(t *testing.T) {
	cfg := onOneDatabase(t)
	again := false
	screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "screen", Routes: func(s httpx.Surfaces) {
			// The same operation, mounted at an address that moves: nothing about
			// either registration fails a gate on its own.
			path := "/note"
			if again {
				path = "/later"
			}
			again = true
			httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: path},
				httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
					return &deskOut{Note: "a note that moved"}, nil
				})
		}}, nil
	})

	rt, err := pkit.NewApp("collect").Use(doors, screen).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
	}
	if err == nil {
		t.Fatal("a composition that mounted /note when it was gated and /later when it was built was accepted")
	}
	says(t, err, "/note")
	says(t, err, "/later")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused second registration left %d tables behind: the migration ran before the two registrations were compared", got)
	}
}
