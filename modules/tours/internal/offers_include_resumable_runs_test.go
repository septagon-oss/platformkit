package internal_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tours"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
	"github.com/septagon-oss/platformkit/modules/tours/internal"
)

const toursHost = "tours.test"

type toursRouteIdentity struct {
	tenant tenancy.Tenant
}

func (r toursRouteIdentity) ByHost(_ context.Context, _ db.Tx[db.System], host string) (tenancy.Tenant, error) {
	if host != toursHost {
		return tenancy.Tenant{}, tenancy.ErrNoSuchHost
	}
	return r.tenant, nil
}

func (toursRouteIdentity) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

func (toursRouteIdentity) Decide(context.Context, tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	return tenancy.PolicyDecision{Allowed: true}, nil
}

func TestOffersIncludeResumableRuns(t *testing.T) {
	_, conn := dbtest.Schema(t, tours.Migrations)
	identity := toursRouteIdentity{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "tours", Name: "Tours"}}
	person := uuid.New()
	clock := func() time.Time { return time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC) }
	app := appname.MustParse("tour-test")
	svc := internal.NewService(clock, identity, app)
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), identity.tenant), person)
	var runID uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		run, err := svc.Start(ctx, tx, contracts.Start{TourID: "intro", Edition: 1, StepIDs: []string{"welcome"}, ContractHash: "contract", Source: "manual"})
		if err != nil {
			return err
		}
		run, err = svc.Act(ctx, tx, person, run.ID, contracts.Action{Kind: "viewed", Step: "welcome", ExpectedRevision: run.Revision})
		if err == nil {
			runID = run.ID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: toursHost, Tenants: identity,
		Conn: conn, Authorize: identity,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: person}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	tours.New(tours.Deps{Clock: clock, Policy: identity, App: app, Service: svc}).Routes(api.Surfaces("tours"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://"+toursHost+"/api/v1/tours/offers?view=workspace", nil)
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), runID.String()) {
		t.Fatalf("offers = %d %s, want the resumable run %s; mounted=%+v", response.Code, response.Body, runID, api.Mounted())
	}
}
