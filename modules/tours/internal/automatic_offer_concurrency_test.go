package internal_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
	"github.com/septagon-oss/platformkit/modules/tours/internal"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"
)

type allowingTourPolicy struct{}

func (allowingTourPolicy) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true}, nil
}

func TestTourModuleDeclaresPlaybackRoutes(t *testing.T) {
	if tours.New(tours.Deps{}).Routes == nil {
		t.Fatal("tour playback endpoints are not registered in the module")
	}
}

func TestTourMigrationPersistsEditionAndOfferState(t *testing.T) {
	sql, err := fs.ReadFile(tours.Migrations.Files, "000001_tours.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"tours_person_state", "tours_person_frequency", "tours_reservations"} {
		if !strings.Contains(string(sql), "CREATE TABLE "+table) {
			t.Errorf("migration does not persist %s", table)
		}
	}
}

func TestTourRunCannotCrossTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	svc := internal.NewService(func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }, allowingTourPolicy{}, appname.MustParse("tour-test"))
	owner, other, person := tenancy.Tenant{ID: uuid.New()}, tenancy.Tenant{ID: uuid.New()}, uuid.New()
	var id uuid.UUID
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), owner), person)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		run, err := svc.Start(ctx, tx, contracts.Start{TourID: "intro", Edition: 1, StepIDs: []string{"welcome"}, ContractHash: "contract", Source: "manual"})
		if err == nil {
			id = run.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ctx = tenancy.WithActor(tenancy.WithTenant(t.Context(), other), person)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Get(ctx, tx, person, id); !errors.Is(err, crud.ErrNotFound) {
			t.Fatalf("cross-tenant read error = %v, want not found", err)
		}
		if _, err := svc.Act(ctx, tx, person, id, contracts.Action{Kind: "exit", ExpectedRevision: 1}); !errors.Is(err, crud.ErrNotFound) {
			t.Fatalf("cross-tenant write error = %v, want not found", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentAutomaticStartsReserveOneOffer(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	app := appname.MustParse("tour-test")
	svc := internal.NewService(func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }, allowingTourPolicy{}, app)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "tour-test", Name: "Tour test"}
	person := uuid.New()
	start := contracts.Start{TourID: "intro", Edition: 1, StepIDs: []string{"welcome"}, ContractHash: "contract", Source: "automatic"}
	gate := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), tenant), person)
			<-gate
			errs[i] = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.Start(ctx, tx, start)
				return err
			})
		}(i)
	}
	close(gate)
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent automatic starts succeeded %d times, want one reservation; errors: %v", wins, errs)
	}
}
