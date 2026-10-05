package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestCacheRefusalReturnsOnlyItsOwnNames(t *testing.T) {
	first := onOneDatabase(t)
	first.NATS.App = "collect"
	live, err := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger")).
		Build(t.Context(), buildDeployment(first, app.All))
	if err != nil {
		t.Fatalf("build the standing composition: %v", err)
	}
	t.Cleanup(func() { _ = live.Close() })
	otherShape := []events.Declared{events.Declare[postedText]("ledger.posted")}
	t.Run("refused", func(t *testing.T) {
		second := onOneDatabase(t)
		if second.Database.URL == first.Database.URL {
			t.Fatal("the two compositions need separate databases")
		}
		second.NATS.App = "collect"
		second.Cache.App = "not/a/slug"
		declaration := events.Declare[postedNumber]("screen.recorded")
		screen := pkit.NewModule("screen", func(*pkit.Wiring) (module.Module, error) {
			return module.Module{Name: "screen", Declared: []events.Declared{declaration}}, nil
		})
		application := pkit.NewApp("collect").Use(doors, desk, postedModule[postedNumber]("ledger"), screen)
		runtime, err := application.Build(t.Context(), buildDeployment(second, app.All))
		if runtime != nil {
			_ = runtime.Close()
			t.Fatal("a cache name that is not a slug returned a runtime")
		}
		if err == nil || !app.RefusedBeforeEffects(err) || !strings.Contains(err.Error(), "cache.app") {
			t.Fatalf("did not reach the pre-effect cache-name refusal: %v", err)
		}
		if count := tablesIn(t, second.Database.MigrateURL); count != 0 {
			t.Fatalf("cache-name refusal migrated %d tables", count)
		}
		corrected := events.Declare[postedText]("screen.recorded")
		if err := events.CheckAppDeclared("collect", []events.Declared{corrected}); err != nil {
			t.Errorf("the refused boot kept its own event name: %v", err)
		}
		if err := events.CheckAppDeclared("collect", otherShape); err == nil {
			t.Error("the refused boot released the standing composition's event name")
		}
		second.Cache.App = "collect"
		declaration = corrected
		runtime, err = application.Build(t.Context(), buildDeployment(second, app.All))
		if err != nil {
			t.Fatalf("the same App could not build the corrected composition: %v", err)
		}
		t.Cleanup(func() { _ = runtime.Close() })
		if err := runtime.Close(); err != nil {
			t.Fatal(err)
		}
		if err := events.CheckAppDeclared("collect", otherShape); err == nil {
			t.Error("closing the corrected boot released the standing composition's event name")
		}
	})
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	if err := events.CheckAppDeclared("collect", otherShape); err != nil {
		t.Errorf("closing the last lifecycle kept its old event name: %v", err)
	}
}
