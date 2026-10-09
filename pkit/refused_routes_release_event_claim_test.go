package pkit_test

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestRefusedRoutesReleaseTheirEventClaims(t *testing.T) {
	for _, at := range []struct {
		name string
		pass int
	}{{"dry_registration", 2}, {"serving_registration", 4}} {
		t.Run(at.name, func(t *testing.T) {
			t.Cleanup(func() { events.DeclareAll(nil) })
			cfg := onOneDatabase(t)
			deployment := buildDeployment(cfg, app.All)
			registrations := 0
			corrected := false
			declaration := events.Declare[ledgerRecorded]("screen.recorded")
			correctedDeclaration := events.Declare[struct {
				Number string `json:"number"`
			}]("screen.recorded")
			screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
				return module.Module{Name: "screen", Declared: []events.Declared{declaration}, Routes: func(s httpx.Surfaces) {
					registrations++
					path := "/note"
					if !corrected && registrations == at.pass {
						path = "/later"
					}
					httpx.Register(s.App, huma.Operation{Method: "GET", OperationID: "screen.note", Path: path},
						httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
							return &deskOut{Note: "a note"}, nil
						})
				}}, nil
			})
			application := pkit.NewApp("collect").Use(doors, screen)
			runtime, err := application.Build(t.Context(), deployment)
			if runtime != nil {
				_ = runtime.Close()
				t.Fatal("inconsistent route registrations returned a runtime")
			}
			if err == nil || !app.RefusedBeforeEffects(err) || registrations != at.pass {
				t.Fatalf("did not reach the pre-effect route refusal on pass %d: registrations=%d, err=%v", at.pass, registrations, err)
			}
			if count := tablesIn(t, cfg.Database.MigrateURL); count != 0 {
				t.Fatalf("route refusal migrated %d tables", count)
			}
			if err := events.CheckAppDeclared("", []events.Declared{correctedDeclaration}); err != nil {
				t.Errorf("the refused composition still holds its event shape: %v", err)
			}
			corrected = true
			declaration = correctedDeclaration
			runtime, err = application.Build(t.Context(), deployment)
			if err != nil {
				t.Fatalf("the corrected composition could not build after a refusal with no effects: %v", err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
