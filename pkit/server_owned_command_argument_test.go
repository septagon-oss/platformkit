package pkit_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBuildRefusesServerOwnedCommandArgumentBeforeMigration(t *testing.T) {
	cfg := onOneDatabase(t)
	calls := 0
	invalid := pkit.NewModule("booking", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "booking", Routes: func(s httpx.Surfaces) {
			spec := rest.Spec[*taskcontracts.Task]{Module: "booking", Entity: "reservation", Path: "/reservations"}
			rest.Command(s, spec, "confirm", "Confirm", "Confirm the reservation.", nil,
				func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct {
					Revision int64 `json:"revision"`
				}) (*taskcontracts.Task, error) {
					calls++
					return nil, nil
				}, rest.CommandOptions{Auth: httpx.SignedIn()})
		}}, nil
	})
	a := pkit.NewApp("invalid-command").Use(doors, invalid)
	var got any
	func() {
		defer func() { got = recover() }()
		runtime, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
		if runtime != nil {
			defer runtime.Close()
		}
		if err != nil {
			t.Errorf("Build returned an error instead of the declaration panic: %v", err)
		}
	}()
	want := `rest: command confirm on booking.reservation: argument field "revision" is server-owned; use a caller-owned argument name instead (ifRevision for an observed revision)`
	if got != want {
		t.Errorf("Build panic = %v, want %q", got, want)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("refused command left %d tables behind", got)
	}
	if calls != 0 {
		t.Errorf("refused command ran %d callbacks", calls)
	}
}
