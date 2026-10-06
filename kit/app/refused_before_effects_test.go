package app

// RefusedBeforeEffects is what a caller reads to keep its own record honest: a layer
// that wrote "this lifecycle is spent" on the way to a boot may take that back only
// for a refusal answered while the deployment was untouched. One Start, two sides.

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// mounting answers with one public route, and with a different one on the
// registration named: a callback that counts its own calls is how a mount guard or a
// flag makes one boot mean two shapes, and which pass it changes on picks which of
// the two answers above the connection refuses it.
func mounting(changedOn int) module.Module {
	pass := 0
	return module.Module{
		Name: "shifting",
		Routes: func(s httpx.Surfaces) {
			pass++
			path := "/hello"
			if pass == changedOn {
				path = "/elsewhere"
			}
			httpx.Register(s.App, huma.Operation{OperationID: "shift", Method: http.MethodGet, Path: path},
				httpx.Public(), func(context.Context, *struct{}) (*helloOut, error) {
					return &helloOut{}, nil
				})
		},
	}
}

// sharedStore names the store the deployment named, with its construction counted:
// a refusal above the connection never asked for it, one below it got as far as it.
func sharedStore(cfg *config.Config, built *int, answer error) Caches {
	cfg.Cache = config.Cache{Adapter: "valkey", App: "acme-stack", URL: "valkey://127.0.0.1:6379"}
	return Caches{Valkey: func(context.Context, config.Cache) (cache.Cache, error) {
		*built++
		if answer != nil {
			return nil, answer
		}
		return cache.Memory("acme-stack"), nil
	}}
}

func TestAGateRefusalNamesItselfAsAnsweredBeforeTheDeployment(t *testing.T) {
	cfg, opts := compose(t)
	built := 0
	opts.Caches = sharedStore(&cfg, &built, nil)
	// Registration 2 is refused by Declarations and registration 4 by the API built
	// to serve; both answers are given over an in-process store, and the mark is what
	// tells a caller the lifecycle it spent getting here is returnable.
	for _, changedOn := range []int{2, 4} {
		a, err := New(t.Context(), cfg, []module.Module{mounting(changedOn)}, opts)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rt, err := a.Start(t.Context())
		if rt != nil {
			_ = rt.Close()
			t.Fatalf("Start handed back a Runtime whose registration %d mounted elsewhere", changedOn)
		}
		if err == nil {
			t.Fatalf("Start accepted a composition whose registration %d mounted elsewhere", changedOn)
		}
		if !RefusedBeforeEffects(err) {
			t.Errorf("the refusal of registration %d carries no pre-effect mark, so its caller would keep a lifecycle it never spent: %v", changedOn, err)
		}
	}
	if built != 0 {
		t.Errorf("Start built the shared store %d times before refusing both changed routes", built)
	}
}

func TestAStoreRefusalDoesNotClaimTheDeploymentWasUntouched(t *testing.T) {
	cfg, opts := compose(t)
	built := 0
	unreachable := errors.New("no valkey answered")
	opts.Caches = sharedStore(&cfg, &built, unreachable)
	a, err := New(t.Context(), cfg, []module.Module{mounting(0)}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rt, err := a.Start(t.Context())
	if rt != nil {
		_ = rt.Close()
		t.Fatal("Start handed back a Runtime whose store it refused to build")
	}
	if err == nil || !errors.Is(err, unreachable) {
		t.Fatalf("Start = %v, want the store constructor's own refusal", err)
	}
	if RefusedBeforeEffects(err) {
		t.Errorf("a refusal answered after the application connection was opened carries the pre-effect mark, so its caller would hand out a lifecycle the deployment already paid for: %v", err)
	}
	if built != 1 {
		t.Errorf("the shared store was built %d times, want the one call that refused", built)
	}
}
