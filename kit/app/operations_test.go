package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestOperationProjectionBelongsOnlyToItsInitiatingPerson(t *testing.T) {
	_, conn := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New()}
	person := uuid.New()
	at := db.Now()
	store := idempotency.PostgresOperations()
	ctx := tenancy.WithTenant(t.Context(), tenant)
	rows := make(map[string]idempotency.Receipt)
	for _, outcome := range []request.Outcome{"", request.Succeeded, request.Failed, request.PendingApproval} {
		r := idempotency.Receipt{Ref: uuid.New(), ID: "save", Tenant: tenant.ID, Caller: person, Person: person, Method: "POST", Route: "/tasks", Revision: 1, StartedAt: at}
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := store.Reserve(ctx, tx, r); err != nil {
				return err
			}
			if outcome != "" {
				got, _, err := store.Transition(ctx, tx, r.Operation(), 1, outcome, at)
				r = got
				return err
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		rows[string(outcome)] = r
	}
	current := person
	loader := fixture{tenant: tenant}
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Conn: conn, Tenants: loader, Authorize: loader,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: current}, true, nil
		}})
	mountOperations(api, func() time.Time { return at })
	read := func(ref string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "http://"+tenantHost+"/api/v1/app/operations/me/"+ref, nil)
		req.Header.Set("Authorization", "Bearer present")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for outcome, r := range rows {
		response := read(r.Ref.String())
		want := 200
		if outcome == "" {
			want = 202
		}
		if response.Code != want || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Errorf("%q = %d %s headers %v", outcome, response.Code, response.Body.String(), response.Header())
		}
		if outcome == "" && response.Header().Get("Retry-After") != "1" {
			t.Error("unknown receipt has no retry hint")
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"tenant", "person", "caller", "payload", "tenantId", "personId"} {
			if _, ok := body[private]; ok {
				t.Errorf("projection exposes %s", private)
			}
		}
		if body["id"] != "save" || body["ref"] != r.Ref.String() {
			t.Errorf("projection = %v", body)
		}
	}
	current = uuid.New()
	other := read(rows["succeeded"].Ref.String())
	missing := read(uuid.NewString())
	if other.Code != 404 || missing.Code != 404 {
		t.Errorf("private %d missing %d", other.Code, missing.Code)
	}
	if response := read("invalid"); response.Code != 400 {
		t.Errorf("malformed = %d", response.Code)
	}
	current = person
	at = at.Add(24 * time.Hour)
	if response := read(rows["succeeded"].Ref.String()); response.Code != 404 {
		t.Errorf("expired = %d", response.Code)
	}
	if response := read(rows["pending_approval"].Ref.String()); response.Code != 200 {
		t.Errorf("pending expired = %d", response.Code)
	}
}
