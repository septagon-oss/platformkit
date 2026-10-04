package httpx_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/telemetry"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestAHostMoveAttributesTheNextRequestToTheNewTenant pins the join between
// the shared host cache and request measurement. The host is the same on both
// requests; only the resolver's answer changes after invalidation.
func TestAHostMoveAttributesTheNextRequestToTheNewTenant(t *testing.T) {
	old := tenancy.Tenant{ID: uuid.MustParse("6ad31b48-92b0-4df4-a64e-d3c18bbdb506"), Slug: "old"}
	newTenant := tenancy.Tenant{ID: uuid.MustParse("37617fa3-79f9-4c5d-bf9a-5328c2e4e1f9"), Slug: "new"}
	current := old
	_, conn := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		PublicHost: host,
		Conn:       conn,
		Cache:      cache.Memory("pkit"),
		Tenants: loaderFunc(func(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			return current, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	const operation = "host_move_measurement"
	httpx.Register(api.Surfaces("hostmove").App, huma.Operation{
		OperationID: operation, Method: http.MethodGet, Path: "/reading",
	}, httpx.Public(), func(context.Context, *struct{}) (*struct{}, error) {
		return &struct{}{}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("declare the route: %v", err)
	}

	serve := func(want tenancy.Tenant) {
		t.Helper()
		spans.Reset()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
			"http://"+host+"/api/v1/hostmove/reading", nil))
		if response.Code != http.StatusNoContent || response.Header().Get(httpx.RequestIDHeader) == "" {
			t.Fatalf("the declared route answered %d with request id %q: %s",
				response.Code, response.Header().Get(httpx.RequestIDHeader), response.Body.String())
		}
		span := serverSpan(t)
		for key, value := range map[string]string{
			telemetry.AttrTenant: want.Slug, telemetry.AttrTenantID: want.ID.String(),
		} {
			got, ok := attributeOf(span, key)
			if !ok || got.AsString() != value {
				t.Errorf("server span %s = %q (present %v), want %q", key, got.AsString(), ok, value)
			}
		}
	}

	serve(old)
	current = newTenant
	if err := api.InvalidateHost(host); err != nil {
		t.Fatalf("invalidate the moved host: %v", err)
	}
	serve(newTenant)

	series := map[string]uint64{}
	for _, point := range histogramPoints(t, "pkit.http.operation.duration") {
		op, ok := point.Attributes.Value(attribute.Key("pkit.operation"))
		if !ok || op.AsString() != operation {
			continue
		}
		slug, hasSlug := point.Attributes.Value(attribute.Key(telemetry.AttrTenant))
		id, hasID := point.Attributes.Value(attribute.Key(telemetry.AttrTenantID))
		if !hasSlug || !hasID {
			t.Errorf("operation duration has no tenant keys: %v", point.Attributes.ToSlice())
			continue
		}
		series[slug.AsString()+"/"+id.AsString()] += point.Count
	}
	for _, want := range []tenancy.Tenant{old, newTenant} {
		key := want.Slug + "/" + want.ID.String()
		if series[key] != 1 {
			t.Errorf("duration series for %s has %d requests, want one", key, series[key])
		}
	}
	if len(series) != 2 {
		t.Errorf("the moved host made %d tenant series, want exactly two: %v", len(series), series)
	}
}
