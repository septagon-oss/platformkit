package pkit_test

// The other half of "a build refused before the first effect changes nothing in the
// process it was asked in": a boot that got past the gates and reached the pool has
// spent this App's one lifecycle even though it came back with no Runtime, and a
// second Build of the same App must say so rather than migrate a second time over
// the same configuration. The refusal below is the shared store's own, which the
// engine meets after it has opened the application connection.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestARefusalThatReachedThePoolLeavesTheAppSpent(t *testing.T) {
	cfg := onOneDatabase(t)
	cfg.Cache = config.Cache{Adapter: "valkey", App: "collect", URL: "valkey://127.0.0.1:6379"}
	d := buildDeployment(cfg, app.All)
	d.Caches = app.Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) {
		return nil, errors.New("no valkey answered")
	}}
	a := pkit.NewApp("collect").Use(doors, desk)

	rt, err := a.Build(t.Context(), d)
	if rt != nil {
		defer rt.Close()
		t.Fatal("Build returned a runtime whose shared store refused to open")
	}
	if err == nil || !strings.Contains(err.Error(), "no valkey answered") {
		t.Fatalf("Build did not refuse the shared store: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Fatalf("the refused build migrated %d tables", got)
	}

	rt, err = a.Build(t.Context(), d)
	if rt != nil {
		defer rt.Close()
		t.Fatal("a second Build started a lifecycle on an App whose boot reached the pool")
	}
	if err == nil || !strings.Contains(err.Error(), "already built") {
		t.Errorf("a second Build of an App whose boot reached the pool = %v, want the one-lifecycle refusal", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the second build migrated %d tables", got)
	}
}
