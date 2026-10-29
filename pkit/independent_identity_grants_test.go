package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	identityuser "github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestUserConstructsWithoutASessionService(t *testing.T) {
	p, err := pkit.NewApp("identity").Use(identityuser.Module, auth.Grants, doors, desk).
		Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := pkit.Value[usercontracts.Lookup](p); !ok || v == nil {
		t.Fatal("User was not constructed")
	}
	if _, ok := pkit.Value[authcontracts.Auth](p); ok {
		t.Fatal("User construction acquired a session service")
	}
	if v, ok := pkit.Value[authcontracts.PermissionReader](p); !ok || v == nil {
		t.Fatal("independent reader missing")
	}
	g, ok := pkit.Value[usercontracts.Granting](p)
	if !ok || g == nil {
		t.Fatal("independent granting missing")
	}
	if allowed, err := g.May(t.Context(), db.Tx[db.Tenant]{}); allowed || err == nil || strings.Contains(err.Error(), "no Ask") {
		t.Fatalf("initialized granting without a transaction = %v, %v", allowed, err)
	}
	order := map[string]int{}
	for i, m := range p.Modules() {
		order[m.Name] = i
	}
	if order["authgrants"] >= order["user"] {
		t.Fatalf("grant dependency order: %v", order)
	}
}

func TestMissingOrAmbiguousIdentityGrantsFailBeforeBuilders(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		built := false
		probe := pkit.NewModule("probe", func(*pkit.Wiring) (module.Module, error) {
			built = true
			return module.Module{Name: "probe"}, nil
		})
		app := pkit.NewApp("identity").Use(identityuser.Module, doors, desk, probe)
		if duplicate {
			other := pkit.NewModule("othergrants", func(*pkit.Wiring) (module.Module, error) {
				built = true
				return module.Module{Name: "othergrants"}, nil
			}, pkit.Provides[usercontracts.Granting]())
			app.Use(auth.Grants, other)
		}
		if _, err := app.Plan(pkit.Deployment{Environment: pkit.Development}); err == nil {
			t.Fatal("invalid grant graph was accepted")
		}
		if built {
			t.Fatal("invalid graph ran a builder")
		}
	}
}
