package pkit_test

// Decision 0074 rule 1 lists what Build's validate phase answers before it
// migrates: "roles, and the existing module, route and event gates, on a dry
// composition. Only then does it migrate." The route gates are the kernel's
// own — an operation guarded by a permission no composed module defines, and a
// composition whose workspace mounts nothing — and each of them is a refusal
// Build gives. These cases hold Build to the rule's order: the refusal comes
// back and the schema it was pointed at is still empty.

import (
	"context"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// ledger guards its one screen with a permission that no composed module
// defines, which the kernel's route gate refuses.
var ledger = pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "ledger", Routes: func(r httpx.Surfaces) {
		httpx.Register(r.App, huma.Operation{Method: "GET", OperationID: "ledger.note", Path: "/"},
			httpx.Permission("ledger:read"), func(context.Context, *deskIn) (*deskOut, error) {
				return &deskOut{Note: "a ledger nobody may read"}, nil
			})
	}}, nil
})

func TestBuildRefusesAnUndefinedPermissionBeforeAnyEffect(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, ledger)

	_, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	says(t, err, "ledger:read")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind: the route gate answered after the migration ran", got)
	}
}

func TestBuildRefusesAnEmptyWorkspaceBeforeAnyEffect(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors)

	_, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	says(t, err, "workspace")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind: the workspace gate answered after the migration ran (%v)", got, err)
	}
}
