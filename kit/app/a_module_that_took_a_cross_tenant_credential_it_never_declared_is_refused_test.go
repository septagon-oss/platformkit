package app

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// rollUp is the response body of the one route crosserModule mounts: its shape
// is nothing and its existence is the point — a module that crosses tenants has
// to register the route where the crossing happens.
type rollUp struct {
	Body struct {
		Tenants int `json:"tenants"`
	}
}

// crosserModule is a module with one workspace route and, optionally, a declared
// cross-tenant need and a call that takes the credential. The two cases below
// differ only in those two booleans, which is the whole of the rule: a need the
// manifest states and a token the module takes are one fact stated twice, and
// either half alone is a composition that lies about itself.
func crosserModule(declares, takes bool) module.Module {
	m := module.Module{Name: "crosser"}
	if declares {
		m.SystemAccess = module.SystemAccess{Reason: "read every tenant's roll-up, which belongs to no tenant"}
	}
	m.Routes = func(s httpx.Surfaces) {
		if takes {
			_ = s.App.SystemToken()
		}
		httpx.Register(s.App, huma.Operation{
			OperationID: "crosser-roll-up", Method: http.MethodGet, Path: "/roll-up",
			Summary: "The roll-up across every tenant",
		}, httpx.SignedIn(), func(context.Context, *struct{}) (*rollUp, error) { return nil, nil })
	}
	return m
}

// refusedAboveTheConnection composes the module and answers what the boot said,
// failing the test when the answer arrived after the deployment was dialed.
func refusedAboveTheConnection(t *testing.T, m module.Module) string {
	t.Helper()
	cfg, opts := compose(t)
	undialableDatabase(&cfg)
	a, err := New(t.Context(), cfg, []module.Module{m}, opts)
	if err != nil {
		t.Fatalf("compose the module: %v", err)
	}
	err = a.Declarations()
	if err == nil {
		t.Fatal("the composition answered that it may serve")
	}
	if !RefusedBeforeEffects(err) {
		t.Errorf("the refusal arrived below the line where the connection is opened, so the refused boot spent the deployment: %v", err)
	}
	return err.Error()
}

func TestAModuleThatTookACrossTenantCredentialItNeverDeclaredIsRefused(t *testing.T) {
	said := refusedAboveTheConnection(t, crosserModule(false, true))
	for _, want := range []string{"crosser", "SystemAccess"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q, so whoever wrote the manifest cannot find the line to fix: %s", want, said)
		}
	}
}

func TestADeclaredCrossTenantNeedNothingTookIsRefused(t *testing.T) {
	said := refusedAboveTheConnection(t, crosserModule(true, false))
	for _, want := range []string{"crosser", "never took its token"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal does not name %q: %s", want, said)
		}
	}
}

func TestACredentialDeclaredAndTakenBoots(t *testing.T) {
	cfg, opts := compose(t)
	undialableDatabase(&cfg)
	a, err := New(t.Context(), cfg, []module.Module{crosserModule(true, true)}, opts)
	if err != nil {
		t.Fatalf("compose the module: %v", err)
	}
	if err := a.Declarations(); err != nil {
		t.Fatalf("the composition refused a module that declared its cross-tenant work and took its token: %v", err)
	}
}
