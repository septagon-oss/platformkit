package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestOperationPreservesProjectionsAndAccess(t *testing.T) {
	api, router, _ := mountAs(t, spec, refuses{})
	type input struct {
		Slug string `path:"slug"`
		Page int    `query:"page" minimum:"1"`
	}
	type projection struct {
		Slug  string    `json:"slug"`
		Page  int       `json:"page"`
		Actor uuid.UUID `json:"actor"`
	}
	calls := 0
	for name, access := range map[string]httpx.Auth{"public": httpx.Public(), "mine": httpx.SignedIn(), "staff": httpx.Permission("task:read")} {
		rest.Operation(api, huma.Operation{OperationID: "projection-" + name, Method: http.MethodGet, Path: "/projections/" + name + "/{slug}", DefaultStatus: http.StatusAccepted}, access,
			func(_ context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, in *input) ([]projection, error) {
				calls++
				if db.TenantOf(tx).ID != acme.ID {
					t.Fatal("projection received another tenant's transaction")
				}
				return []projection{{Slug: in.Slug, Page: in.Page, Actor: actor}}, nil
			}, rest.OperationOptions{CacheControl: "private, no-store"})
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		auth bool
		want int
	}{
		{"public", false, 202}, {"public", true, 202},
		{"mine", false, 403}, {"mine", true, 202}, {"staff", true, 403},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/projections/"+tc.name+"/report?page=2", nil)
		if tc.auth {
			req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("%s authenticated=%v: %d %s", tc.name, tc.auth, out.Code, out.Body)
		}
		if tc.want != 202 {
			continue
		}
		var body []projection
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil || len(body) != 1 || body[0].Slug != "report" || body[0].Page != 2 {
			t.Fatalf("custom list projection: %s (%v)", out.Body, err)
		}
		actor := uuid.Nil
		if tc.auth {
			actor = principal
		}
		if body[0].Actor != actor || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("actor/cache policy: %s %v", out.Body, out.Header())
		}
	}
	// Public routes without a resolved tenant must not invoke a tenant service.
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "http://unknown.test/projections/public/report?page=2", nil))
	if out.Code != 503 || calls != 3 {
		t.Fatalf("missing tenant: %d; service calls=%d", out.Code, calls)
	}
}

func TestOperationPreservesFaultsAndTransactionOutcome(t *testing.T) {
	api, router, admin := mounted(t)
	type input struct {
		Body struct {
			Title string `json:"title" minLength:"1"`
		}
	}
	rest.Operation(api, huma.Operation{OperationID: "submit-review", Method: http.MethodPost, Path: "/reviews/submit", DefaultStatus: http.StatusCreated,
		Extensions: map[string]any{httpx.EventsExtension: []string{"reviews.submitted"}, "x-review-contract": "v1"}}, httpx.SignedIn(),
		func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *input) (string, error) {
			row := &Task{Title: in.Body.Title}
			if err := crud.Create(ctx, tx, row); err != nil {
				return "", err
			}
			if err := events.Publish(ctx, tx, "reviews.submitted", row.ID); err != nil {
				return "", err
			}
			if in.Body.Title == "stale" {
				return "", fmt.Errorf("%w: review changed", crud.ErrConflict)
			}
			return "accepted", nil
		}, rest.OperationOptions{CacheControl: "no-store"})
	if err := api.ValidateDeclarations(); err != nil || !slices.Contains(api.Events(), "reviews.submitted") {
		t.Fatalf("operation declarations: %v, events=%v", err, api.Events())
	}
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"title":""}`, 422}, {`{"title":"stale"}`, 409}, {`{"title":"ready"}`, 201},
	} {
		if code, body := call(t, router, http.MethodPost, "/reviews/submit", tc.body); code != tc.want {
			t.Fatalf("submit %s: %d %s", tc.body, code, body)
		}
	}
	var rows, published int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM rest_tasks").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = 'reviews.submitted'").Scan(&published); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || published != 1 {
		t.Fatalf("rollback/commit: rows=%d events=%d, want one accepted operation", rows, published)
	}
	ops := api.Recorded()
	i := slices.IndexFunc(ops, func(op *huma.Operation) bool { return op.OperationID == "submit-review" })
	if i < 0 || ops[i].Extensions["x-review-contract"] != "v1" || ops[i].Path != "/reviews/submit" {
		t.Fatalf("custom operation changed: %+v", ops)
	}
}
